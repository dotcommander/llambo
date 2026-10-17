package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/llambo/providers"
)

// syntheticIntentSource writes an offline export whose tokens are axis
// vectors: chat words on axis 0, code on 1, extraction on 2, long_context on
// 3, embeddings on 4 (dim 8).
func syntheticIntentSource(t *testing.T, dir string) {
	t.Helper()
	tokens := []string{"hello", "hi", "function", "bug", "parser", "refactor", "extract", "summarize", "document", "manual", "embedding", "vector"}
	axis := map[string]int{"hello": 0, "hi": 0, "function": 1, "bug": 1, "parser": 1, "refactor": 1, "extract": 2, "summarize": 2, "document": 3, "manual": 3, "embedding": 4, "vector": 4}
	dim := 8

	var vocab strings.Builder
	weights := make([]float32, len(tokens)*dim)
	for id, tok := range tokens {
		vocab.WriteString(tok + "\n")
		weights[id*dim+axis[tok]] = 1
	}
	if err := os.WriteFile(filepath.Join(dir, "vocab.txt"), []byte(vocab.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dim.txt"), []byte("8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := make([]byte, len(weights)*4)
	for i, w := range weights {
		binary.LittleEndian.PutUint32(bin[i*4:], math.Float32bits(w))
	}
	if err := os.WriteFile(filepath.Join(dir, "embeddings.f32.bin"), bin, 0o600); err != nil {
		t.Fatal(err)
	}
}

const syntheticIntentSeeds = `{"intent": "chat", "prompt": "hello there hi"}
{"intent": "code", "prompt": "refactor this parser bug"}
{"intent": "extraction", "prompt": "extract the dates and summarize"}
{"intent": "long_context", "prompt": "read the full document and manual"}
{"intent": "embeddings", "prompt": "compute the embedding vector"}
`

func TestRouteIntentsBuildEndToEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := t.TempDir()
	syntheticIntentSource(t, source)

	seedsPath := filepath.Join(dir, "seeds.jsonl")
	if err := os.WriteFile(seedsPath, []byte(syntheticIntentSeeds), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runRouteIntentsBuild(&commandIO{ctx: context.Background(), stdout: &out}, dir, source, seedsPath); err != nil {
		t.Fatalf("build failed: %v\noutput:\n%s", err, out.String())
	}

	for _, name := range []string{"embeddings.bin", "vocab.json", "manifest.json", "centroids.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s in %s: %v", name, dir, err)
		}
	}

	assets, err := providers.LoadIntentAssetsStrict(dir)
	if err != nil {
		t.Fatalf("reload built assets: %v", err)
	}
	if got := len(assets.Intents()); got != 5 {
		t.Fatalf("built assets carry %d intents, want 5", got)
	}

	cfg := &providers.IntentRouterConfig{Enabled: true, MinTop1: 0.5, MinMargin: 0.2}
	tests := []struct {
		query  string
		want   providers.RoutingIntent
		static bool
	}{
		{"refactor this parser bug", providers.IntentCode, true},
		{"hello hi there", providers.IntentChat, true},
		{"extract and summarize the report", providers.IntentExtraction, true},
		{"compute the embedding vector now", providers.IntentEmbeddings, true},
	}
	for _, tt := range tests {
		res := providers.ResolveIntent("", tt.query, assets, cfg)
		if res.Intent != tt.want || (tt.static && res.Source != providers.IntentStatic) {
			t.Fatalf("ResolveIntent(%q) = %+v, want %s static", tt.query, res, tt.want)
		}
	}
}

func TestRouteIntentsBuildWritesStarterSeedsWithoutSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var out bytes.Buffer
	err := runRouteIntentsBuild(&commandIO{ctx: context.Background(), stdout: &out}, dir, "", "")
	if err == nil || !strings.Contains(err.Error(), "--source") {
		t.Fatalf("err = %v, want missing-assets error mentioning --source", err)
	}
	seedsPath := filepath.Join(dir, "seeds.jsonl")
	data, err := os.ReadFile(seedsPath)
	if err != nil {
		t.Fatalf("starter seeds not written: %v", err)
	}
	if lines := strings.Count(string(data), "\n"); lines < 25 {
		t.Fatalf("starter seeds carry %d lines, want a full starter set", lines)
	}
}
