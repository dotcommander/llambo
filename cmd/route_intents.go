package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "embed"

	"github.com/dotcommander/llambo/providers"
)

//go:embed route_intents_seeds.jsonl
var defaultIntentSeeds []byte

type routeIntentsCommand struct {
	Build routeIntentsBuildCommand `cmd:""`
}

type routeIntentsBuildCommand struct {
	Dir     string   `help:"Router assets directory (default ~/.config/llambo/router)"`
	Source  string   `help:"Export source directory containing vocab.txt, dim.txt, and embeddings.f32.bin (offline export; never fetched from the network)"`
	Seeds   string   `help:"Seeds JSONL path (default <dir>/seeds.jsonl)"`
	Ignored []string `arg:"" optional:"" hidden:""`
}

func (c *routeIntentsBuildCommand) Run(io *commandIO) error {
	return runRouteIntentsBuild(io, c.Dir, c.Source, c.Seeds)
}

type intentSeed struct {
	Intent string `json:"intent"`
	Prompt string `json:"prompt"`
}

func runRouteIntentsBuild(io *commandIO, dirFlag, source, seedsFlag string) error {
	out := io.OutOrStdout()
	dir := dirFlag
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".config", "llambo", "router")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // reviewed directory mode
		return fmt.Errorf("create router assets dir: %w", err)
	}

	seedsPath := seedsFlag
	if seedsPath == "" {
		seedsPath = filepath.Join(dir, "seeds.jsonl")
		if _, err := os.Stat(seedsPath); errors.Is(err, fs.ErrNotExist) {
			if err := os.WriteFile(seedsPath, defaultIntentSeeds, 0o600); err != nil { //nolint:gosec // reviewed file mode
				return fmt.Errorf("write starter seeds: %w", err)
			}
			fmt.Fprintf(out, "Wrote starter seeds: %s\n", seedsPath)
		}
	}

	if source != "" {
		if err := exportStaticSource(source, dir, out); err != nil {
			return err
		}
	}

	assets, err := providers.LoadIntentAssetsStrict(dir)
	if err != nil {
		return fmt.Errorf("load router assets (run with --source <export-dir> to install embeddings first): %w", err)
	}

	seeds, err := readIntentSeeds(seedsPath)
	if err != nil {
		return err
	}
	centroids, counts, unresolved, err := buildCentroids(assets, seeds)
	if err != nil {
		return err
	}

	centroidsPath := filepath.Join(dir, "centroids.json")
	payload, err := json.MarshalIndent(intentCentroidsDoc{
		Version: 1,
		Dim:     assets.Dim(),
		Intents: centroids,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode centroids: %w", err)
	}
	if err := os.WriteFile(centroidsPath, append(payload, '\n'), 0o600); err != nil { //nolint:gosec // reviewed file mode
		return fmt.Errorf("write centroids: %w", err)
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintf(out, "Built intent centroids: %s\n", centroidsPath)
	for _, name := range names {
		fmt.Fprintf(out, "  %-14s %d seeds\n", name, counts[name])
	}
	if unresolved > 0 {
		fmt.Fprintf(out, "  %d seed(s) had no in-vocabulary tokens and were skipped\n", unresolved)
	}
	return nil
}

type intentCentroidsDoc struct {
	Version int                  `json:"version"`
	Dim     int                  `json:"dim"`
	Intents map[string][]float32 `json:"intents"`
}

// exportStaticSource installs an offline static-embedding export into the
// router assets dir. Expected source layout: vocab.txt (one token per line,
// row id = line number), dim.txt (integer), embeddings.f32.bin (little-endian
// float32, vocab_size × dim). Subword markers ("▁", "##") are stripped and
// tokens lowercased so the runtime word tokenizer can match them.
func exportStaticSource(source, dir string, out io.Writer) error {
	vocabLines, err := readLines(filepath.Join(source, "vocab.txt"))
	if err != nil {
		return fmt.Errorf("read source vocab.txt: %w", err)
	}
	dimLine, err := readLines(filepath.Join(source, "dim.txt"))
	if err != nil {
		return fmt.Errorf("read source dim.txt: %w", err)
	}
	if len(dimLine) != 1 {
		return fmt.Errorf("dim.txt must contain exactly one integer line")
	}
	dim, err := strconv.Atoi(strings.TrimSpace(dimLine[0]))
	if err != nil || dim <= 0 {
		return fmt.Errorf("dim.txt must be a positive integer")
	}

	vocab := make(map[string]int, len(vocabLines))
	skipped := 0
	for id, raw := range vocabLines {
		tok := normalizeStaticToken(raw)
		if tok == "" {
			if strings.TrimSpace(raw) != "" {
				skipped++
			}
			continue
		}
		if _, dup := vocab[tok]; dup {
			skipped++
			continue
		}
		vocab[tok] = id
	}
	if len(vocab) == 0 {
		return fmt.Errorf("source vocab.txt produced no usable tokens")
	}

	bin, err := os.ReadFile(filepath.Join(source, "embeddings.f32.bin"))
	if err != nil {
		return fmt.Errorf("read source embeddings.f32.bin: %w", err)
	}
	if len(vocabLines)*dim*4 != len(bin) {
		return fmt.Errorf("embeddings.f32.bin is %d bytes; vocab %d × dim %d expects %d (row count follows vocab.txt line count)", len(bin), len(vocabLines), dim, len(vocabLines)*dim*4)
	}

	binPath := filepath.Join(dir, "embeddings.bin")
	if err := os.WriteFile(binPath, bin, 0o600); err != nil { //nolint:gosec // reviewed file mode
		return fmt.Errorf("write embeddings: %w", err)
	}
	vocabJSON, err := json.Marshal(vocab)
	if err != nil {
		return fmt.Errorf("encode vocab: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vocab.json"), vocabJSON, 0o600); err != nil { //nolint:gosec // reviewed file mode
		return fmt.Errorf("write vocab: %w", err)
	}

	sum := sha256.Sum256(bin)
	manifest := map[string]any{
		"version":           1,
		"dim":               dim,
		"vocab_size":        len(vocabLines),
		"embeddings_sha256": hex.EncodeToString(sum[:]),
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(manifestJSON, '\n'), 0o600); err != nil { //nolint:gosec // reviewed file mode
		return fmt.Errorf("write manifest: %w", err)
	}

	fmt.Fprintf(out, "Exported static embeddings: %d tokens, dim %d (%d duplicate/blank lines skipped)\n", len(vocab), dim, skipped)
	fmt.Fprintf(out, "  %s\n", binPath)
	return nil
}

func normalizeStaticToken(raw string) string {
	tok := strings.ToLower(strings.TrimSpace(raw))
	for _, prefix := range []string{"▁", "##"} {
		tok = strings.TrimPrefix(tok, prefix)
	}
	return strings.TrimSpace(tok)
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	return raw, nil
}

func readIntentSeeds(path string) ([]intentSeed, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read seeds: %w", err)
	}
	known := make(map[string]bool, 0)
	for _, intent := range providers.KnownIntents() {
		known[string(intent)] = true
	}
	var seeds []intentSeed
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var seed intentSeed
		if err := json.Unmarshal([]byte(line), &seed); err != nil {
			return nil, fmt.Errorf("seeds line %d: %w", i+1, err)
		}
		if !known[seed.Intent] {
			return nil, fmt.Errorf("seeds line %d: unknown intent %q (valid: chat, code, extraction, long_context, embeddings)", i+1, seed.Intent)
		}
		if strings.TrimSpace(seed.Prompt) == "" {
			return nil, fmt.Errorf("seeds line %d: empty prompt", i+1)
		}
		seeds = append(seeds, seed)
	}
	if len(seeds) == 0 {
		return nil, fmt.Errorf("seeds file %s carries no seeds", path)
	}
	return seeds, nil
}

// buildCentroids embeds every seed, mean-pools per intent, and normalizes.
// Every known intent must end up with at least one resolved seed.
func buildCentroids(assets *providers.IntentAssets, seeds []intentSeed) (map[string][]float32, map[string]int, int, error) {
	dim := assets.Dim()
	sums := make(map[string][]float32)
	counts := make(map[string]int)
	unresolved := 0
	for _, seed := range seeds {
		vec, ok := assets.Embed(seed.Prompt)
		if !ok {
			unresolved++
			continue
		}
		sum, ok := sums[seed.Intent]
		if !ok {
			sum = make([]float32, dim)
			sums[seed.Intent] = sum
		}
		for d := 0; d < dim; d++ {
			sum[d] += vec[d]
		}
		counts[seed.Intent]++
	}

	known := providers.KnownIntents()
	var missing []string
	for _, intent := range known {
		if counts[string(intent)] == 0 {
			missing = append(missing, string(intent))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, nil, 0, fmt.Errorf("no seeds resolved for intents: %s (add seeds or widen the vocabulary)", strings.Join(missing, ", "))
	}

	centroids := make(map[string][]float32, len(known))
	for _, intent := range known {
		centroids[string(intent)] = unitCentroid(sums[string(intent)], counts[string(intent)])
	}
	return centroids, counts, unresolved, nil
}

func unitCentroid(sum []float32, count int) []float32 {
	out := make([]float32, len(sum))
	inv := 1.0 / float32(count)
	var sumSq float64
	for d := range sum {
		v := sum[d] * inv
		out[d] = v
		sumSq += float64(v) * float64(v)
	}
	if sumSq <= 1e-12 {
		return out
	}
	invNorm := 1.0 / math.Sqrt(sumSq)
	for d := range out {
		out[d] = float32(float64(out[d]) * invNorm)
	}
	return out
}
