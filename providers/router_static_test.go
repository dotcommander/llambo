package providers

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEstimatePromptTokensWordFloor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		system  string
		user    string
		wantMin int
		wantMax int
	}{
		{"word floor dominates", "", "a b c d e f", 6, 6},
		{"char estimate dominates", "", strings.Repeat("x", 400), 100, 100},
		{"cjk bytes dominate", "", "日本語テスト", 5, 5},
		{"combined strings", "system prompt here", "and a user turn", 9, 9},
		{"empty", "", "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EstimatePromptTokens(tt.system, tt.user)
			if got < tt.wantMin || got > tt.wantMax {
				t.Fatalf("EstimatePromptTokens(%q, %q) = %d, want [%d,%d]", tt.system, tt.user, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestTokenizeStatic(t *testing.T) {
	t.Parallel()
	got := tokenizeStatic("Hello, World! it's 2026-10-14")
	want := []string{"hello", "world", "it", "s", "2026", "10", "14"}
	if len(got) != len(want) {
		t.Fatalf("tokenizeStatic = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokenizeStatic[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestIntentAssetsEmbedMeanPoolNorm(t *testing.T) {
	t.Parallel()
	assets := &IntentAssets{
		vocab:   map[string]int{"aa": 0, "bb": 1},
		weights: []float32{2, 0, 0, 2},
		dim:     2,
	}
	vec, ok := assets.Embed("aa bb")
	if !ok {
		t.Fatal("expected embed to resolve")
	}
	// mean of (2,0) and (0,2) = (1,1); L2-normalized = (0.7071, 0.7071)
	want := float32(1.0 / math.Sqrt2)
	for _, v := range vec {
		if math.Abs(float64(v-want)) > 1e-6 {
			t.Fatalf("embed = %v, want components near %.4f", vec, want)
		}
	}
	if _, ok := assets.Embed("zz zz"); ok {
		t.Fatal("expected OOV-only text to fail embed")
	}
}

func TestMatchCentroidMargin(t *testing.T) {
	t.Parallel()
	centroids := [][]float32{{1, 0}, {0, 1}, {0.6, 0.8}}
	idx, top1, margin := matchCentroid([]float32{0.6, 0.8}, centroids)
	if idx != 2 {
		t.Fatalf("idx = %d, want 2", idx)
	}
	if math.Abs(float64(top1-1.0)) > 1e-6 {
		t.Fatalf("top1 = %f, want 1.0", top1)
	}
	if math.Abs(float64(margin-0.2)) > 1e-6 {
		t.Fatalf("margin = %f, want 0.2", margin)
	}
}

func TestCapabilityFitAmbiguityNeutral(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		caps  []string
		timer RoutingIntent
		ambig bool
		want  float64
	}{
		{"confident match", []string{"code"}, IntentCode, false, 1.0},
		{"confident mismatch", []string{"code"}, IntentChat, false, 0.3},
		{"ambiguous is neutral", []string{"code"}, IntentChat, true, 0.5},
		{"no tags stays neutral", nil, IntentChat, false, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := capabilityFit(tt.caps, tt.timer, tt.ambig); got != tt.want {
				t.Fatalf("capabilityFit = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestResolveIntentContract(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRouterAssetsFixture(t, dir, false, false)
	assets, err := LoadIntentAssetsStrict(dir)
	if err != nil {
		t.Fatalf("load fixture assets: %v", err)
	}
	confidentCfg := &IntentRouterConfig{Enabled: true, MinTop1: 0.5, MinMargin: 0.2}

	tests := []struct {
		name          string
		system        string
		user          string
		assets        *IntentAssets
		cfg           *IntentRouterConfig
		wantIntent    RoutingIntent
		wantSource    IntentSource
		wantAmbiguous bool
	}{
		{"no assets keeps lexical code", "", "debug this function", nil, nil, IntentCode, IntentLexical, false},
		{"no assets chat fallback is ambiguous", "", "tell me about mountains", nil, nil, IntentChat, IntentFallback, true},
		{"static confident overrides lexical", "", "chatword chatword chatword", assets, confidentCfg, IntentChat, IntentStatic, false},
		{"static margin failure falls back", "", "chatword codeword", assets, confidentCfg, IntentChat, IntentFallback, true},
		{"disabled cfg keeps lexical", "", "debug this function", assets, &IntentRouterConfig{}, IntentCode, IntentLexical, false},
		{"long context gate skips embedding", "", strings.Repeat("x", longContextByteThreshold+1), assets, confidentCfg, IntentLongCtx, IntentLexical, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := ResolveIntent(tt.system, tt.user, tt.assets, tt.cfg)
			if res.Intent != tt.wantIntent || res.Source != tt.wantSource || res.Ambiguous != tt.wantAmbiguous {
				t.Fatalf("ResolveIntent = %+v, want intent=%s source=%d ambiguous=%v", res, tt.wantIntent, tt.wantSource, tt.wantAmbiguous)
			}
		})
	}
}

func TestResolveIntentRecordsStaticScores(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRouterAssetsFixture(t, dir, false, false)
	assets, err := LoadIntentAssetsStrict(dir)
	if err != nil {
		t.Fatalf("load fixture assets: %v", err)
	}

	res := ResolveIntent("", "chatword chatword", assets, &IntentRouterConfig{Enabled: true, MinTop1: 0.5, MinMargin: 0.2})
	if res.Source != IntentStatic || res.Top1 < 0.99 || res.Margin < 0.99 {
		t.Fatalf("static resolution = %+v, want confident chat", res)
	}
	if detail := ResolutionDetail(res); !strings.Contains(detail, "static top1=") {
		t.Fatalf("detail %q must carry static scores", detail)
	}

	res = ResolveIntent("", "chatword codeword", assets, &IntentRouterConfig{Enabled: true, MinTop1: 0.99, MinMargin: 0.01})
	if !res.Ambiguous || res.Top1 < 0.70 || res.Top1 > 0.71 {
		t.Fatalf("gated resolution = %+v, want ambiguous with top1≈0.707", res)
	}
}

func TestLoadIntentAssetsDegradation(t *testing.T) {
	t.Parallel()
	t.Run("empty dir returns nil", func(t *testing.T) {
		t.Parallel()
		if got := LoadIntentAssets(t.TempDir()); got != nil {
			t.Fatalf("expected nil assets, got %+v", got)
		}
	})
	t.Run("checksum mismatch fails", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeRouterAssetsFixture(t, dir, true, false)
		_, err := LoadIntentAssetsStrict(dir)
		if err == nil || !strings.Contains(err.Error(), "checksum") {
			t.Fatalf("err = %v, want checksum mismatch", err)
		}
	})
	t.Run("truncated embeddings fail", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeRouterAssetsFixture(t, dir, false, true)
		_, err := LoadIntentAssetsStrict(dir)
		if err == nil {
			t.Fatal("expected size error for truncated embeddings")
		}
	})
	t.Run("valid fixture loads with centroids", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeRouterAssetsFixture(t, dir, false, false)
		assets, err := LoadIntentAssetsStrict(dir)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		intents := assets.Intents()
		if len(intents) != 2 || intents[0] != IntentChat || intents[1] != IntentCode {
			t.Fatalf("intents = %v, want [chat code]", intents)
		}
		if assets.Dim() != 2 {
			t.Fatalf("dim = %d, want 2", assets.Dim())
		}
	})
}

// writeRouterAssetsFixture writes a tiny valid asset set: dim 2, tokens
// "chatword" (axis 0) and "codeword" (axis 1), centroids chat/code on the
// same axes. corruptSHA flips the manifest checksum; truncate drops bytes
// from embeddings.bin.
func writeRouterAssetsFixture(t *testing.T, dir string, corruptSHA, truncate bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	dim := 2
	vocab := map[string]int{"chatword": 0, "codeword": 1}
	weights := []float32{1, 0, 0, 1}
	bin := make([]byte, len(weights)*4)
	for i, w := range weights {
		binary.LittleEndian.PutUint32(bin[i*4:], math.Float32bits(w))
	}
	if truncate {
		bin = bin[:len(bin)-1]
	}
	if err := os.WriteFile(filepath.Join(dir, "embeddings.bin"), bin, 0o600); err != nil {
		t.Fatal(err)
	}
	vocabJSON, _ := json.Marshal(vocab)
	if err := os.WriteFile(filepath.Join(dir, "vocab.json"), vocabJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bin)
	digest := hex.EncodeToString(sum[:])
	if corruptSHA {
		digest = strings.Repeat("0", 64)
	}
	manifest := map[string]any{"version": 1, "dim": dim, "vocab_size": len(vocab), "embeddings_sha256": digest}
	manifestJSON, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	centroids := map[string][]float32{"chat": {1, 0}, "code": {0, 1}}
	centroidsJSON, _ := json.Marshal(map[string]any{"version": 1, "dim": dim, "intents": centroids})
	if err := os.WriteFile(filepath.Join(dir, "centroids.json"), centroidsJSON, 0o600); err != nil {
		t.Fatal(err)
	}
}
