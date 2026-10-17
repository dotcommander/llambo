package providers

// Static intent router (L1): zero-LLM prompt-to-intent resolution using a
// static token-embedding matrix and per-intent unit-length centroids. Assets
// live under ~/.config/llambo/router and are produced by
// `llambo route intents build`; when they are absent or invalid the router
// degrades silently to the lexical intent detector.

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unsafe"
)

// intentAssetsVersion is the on-disk asset format version understood by this
// build. Loaders reject any other version rather than guessing.
const intentAssetsVersion = 1

// IntentAssets is the immutable, loaded static-intent router state. It is
// safe for concurrent read use once loaded.
type IntentAssets struct {
	vocab     map[string]int
	weights   []float32 // VocabSize × Dim, row-major
	dim       int
	intents   []RoutingIntent
	centroids [][]float32 // unit-length, parallel to intents
}

// Dim reports the embedding dimension.
func (a *IntentAssets) Dim() int { return a.dim }

// Intents returns the intent labels in centroid-index order.
func (a *IntentAssets) Intents() []RoutingIntent {
	out := make([]RoutingIntent, len(a.intents))
	copy(out, a.intents)
	return out
}

// KnownIntents returns the canonical intent vocabulary used by capability
// fit scoring. Centroid builds must cover exactly this set.
func KnownIntents() []RoutingIntent {
	return []RoutingIntent{IntentChat, IntentCode, IntentExtraction, IntentLongCtx, IntentEmbeddings}
}

// Embed computes the L2-normalized mean-pooled static embedding for text,
// skipping out-of-vocabulary tokens. It reports false when no token resolves.
func (a *IntentAssets) Embed(text string) ([]float32, bool) {
	tokens := tokenizeStatic(text)
	if len(tokens) == 0 {
		return nil, false
	}
	out := make([]float32, a.dim)
	valid := 0
	for _, tok := range tokens {
		id, ok := a.vocab[tok]
		if !ok || id < 0 || (id+1)*a.dim > len(a.weights) {
			continue
		}
		off := id * a.dim
		for d := 0; d < a.dim; d++ {
			out[d] += a.weights[off+d]
		}
		valid++
	}
	if valid == 0 {
		return nil, false
	}
	var sumSq float32
	invCount := 1.0 / float32(valid)
	for d := range out {
		v := out[d] * invCount
		out[d] = v
		sumSq += v * v
	}
	if sumSq <= 1e-12 {
		return nil, false
	}
	invNorm := float32(1.0 / math.Sqrt(float64(sumSq)))
	for d := range out {
		out[d] *= invNorm
	}
	return out, true
}

// tokenizeStatic lowercases text and splits it into runs of letters and
// digits. Static vocabularies are word-level by construction (the export
// step strips subword markers), so no runtime BPE is needed.
func tokenizeStatic(s string) []string {
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// matchCentroid returns the best centroid index, its cosine score, and the
// margin over the runner-up. Vectors are assumed unit-length, so the dot
// product is the cosine.
func matchCentroid(vec []float32, centroids [][]float32) (bestIdx int, top1, margin float32) {
	bestIdx = -1
	top1 = -1.0
	top2 := float32(-1.0)
	for idx, c := range centroids {
		if len(c) != len(vec) {
			continue
		}
		var dot float32
		for i := range vec {
			dot += vec[i] * c[i]
		}
		if dot > top1 {
			top2 = top1
			top1 = dot
			bestIdx = idx
		} else if dot > top2 {
			top2 = dot
		}
	}
	return bestIdx, top1, top1 - top2
}

type intentAssetsManifest struct {
	Version          int    `json:"version"`
	Dim              int    `json:"dim"`
	VocabSize        int    `json:"vocab_size"`
	EmbeddingsSHA256 string `json:"embeddings_sha256"`
}

type intentCentroidsFile struct {
	Version int                  `json:"version"`
	Dim     int                  `json:"dim"`
	Intents map[string][]float32 `json:"intents"`
}

// LoadIntentAssetsStrict loads router assets from dir, reporting the first
// failure. Centroids are optional at load time (the build command writes
// them separately), but embeddings, vocab, and manifest are required.
func LoadIntentAssetsStrict(dir string) (*IntentAssets, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest intentAssetsManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if manifest.Version != intentAssetsVersion {
		return nil, fmt.Errorf("unsupported asset version %d (want %d)", manifest.Version, intentAssetsVersion)
	}
	if manifest.Dim <= 0 || manifest.VocabSize <= 0 {
		return nil, fmt.Errorf("manifest dim/vocab_size must be positive")
	}

	vocabBytes, err := os.ReadFile(filepath.Join(dir, "vocab.json"))
	if err != nil {
		return nil, fmt.Errorf("read vocab: %w", err)
	}
	var vocab map[string]int
	if err := json.Unmarshal(vocabBytes, &vocab); err != nil {
		return nil, fmt.Errorf("parse vocab: %w", err)
	}
	for tok, id := range vocab {
		if strings.TrimSpace(tok) == "" || id < 0 || id >= manifest.VocabSize {
			return nil, fmt.Errorf("vocab entry %q has out-of-range id %d", tok, id)
		}
	}

	binPath := filepath.Join(dir, "embeddings.bin")
	expectBytes := manifest.VocabSize * manifest.Dim * 4
	weights, err := loadFloat32Matrix(binPath, expectBytes)
	if err != nil {
		return nil, fmt.Errorf("load embeddings: %w", err)
	}
	sum := sha256.New()
	if err := hashFile(sum, binPath); err != nil {
		return nil, fmt.Errorf("hash embeddings: %w", err)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != manifest.EmbeddingsSHA256 {
		return nil, fmt.Errorf("embeddings checksum mismatch (manifest %s, file %s)", manifest.EmbeddingsSHA256, got)
	}

	assets := &IntentAssets{vocab: vocab, weights: weights, dim: manifest.Dim}

	centroidBytes, err := os.ReadFile(filepath.Join(dir, "centroids.json"))
	switch {
	case err == nil:
		var cf intentCentroidsFile
		if err := json.Unmarshal(centroidBytes, &cf); err != nil {
			return nil, fmt.Errorf("parse centroids: %w", err)
		}
		if cf.Version != intentAssetsVersion {
			return nil, fmt.Errorf("unsupported centroids version %d", cf.Version)
		}
		if len(cf.Intents) == 0 {
			return nil, fmt.Errorf("centroids file carries no intents")
		}
		names := make([]string, 0, len(cf.Intents))
		for name := range cf.Intents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			vec := cf.Intents[name]
			if len(vec) != manifest.Dim {
				return nil, fmt.Errorf("centroid %q has dim %d (want %d)", name, len(vec), manifest.Dim)
			}
			assets.intents = append(assets.intents, RoutingIntent(name))
			assets.centroids = append(assets.centroids, unitNormalize(vec))
		}
	case os.IsNotExist(err):
		// Embeddings-only load: the build command resolves centroids later.
	default:
		return nil, fmt.Errorf("read centroids: %w", err)
	}
	return assets, nil
}

// LoadIntentAssets loads router assets, returning nil on any failure. The
// routing hot path uses this form: missing assets mean lexical-only intent
// detection, never an error.
func LoadIntentAssets(dir string) *IntentAssets {
	assets, err := LoadIntentAssetsStrict(dir)
	if err != nil {
		return nil
	}
	return assets
}

var defaultIntentAssets = sync.OnceValue(func() *IntentAssets {
	return LoadIntentAssets(filepath.Join(configDir, "router"))
})

// DefaultIntentAssets returns the process-wide router assets from the config
// directory, loaded once. Nil when absent or invalid.
func DefaultIntentAssets() *IntentAssets { return defaultIntentAssets() }

// loadFloat32Matrix memory-maps a little-endian float32 matrix, falling back
// to an explicit decode when mmap is unavailable. The mapping lives for the
// process lifetime; there is no munmap by design (CLI exit or gateway
// lifetime owns it).
func loadFloat32Matrix(path string, expectBytes int) ([]float32, error) {
	f, err := os.Open(path) //nolint:gosec // path is the operator-configured asset dir
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	size := st.Size()
	if size != int64(expectBytes) {
		f.Close()
		return nil, fmt.Errorf("size %d bytes (want %d for vocab×dim float32)", size, expectBytes)
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	f.Close()
	if err == nil {
		return unsafe.Slice((*float32)(unsafe.Pointer(&data[0])), len(data)/4), nil
	}
	// Fallback: explicit decode (slower cold start, identical semantics).
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := make([]float32, len(raw)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out, nil
}

func hashFile(w io.Writer, path string) error {
	f, err := os.Open(path) //nolint:gosec // path is the operator-configured asset dir
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// unitNormalize returns a unit-length copy of vec. All-zero vectors come
// back unchanged; loaders treat them as degenerate centroids.
func unitNormalize(vec []float32) []float32 {
	out := append([]float32(nil), vec...)
	var sumSq float64
	for _, v := range out {
		sumSq += float64(v) * float64(v)
	}
	if sumSq <= 1e-12 {
		return out
	}
	inv := 1.0 / math.Sqrt(sumSq)
	for i := range out {
		out[i] = float32(float64(out[i]) * inv)
	}
	return out
}
