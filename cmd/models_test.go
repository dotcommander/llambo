package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

func TestModelsListHelpDescribesInstantPath(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"models", "list", "--help"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"immediately",
		"never calls provider APIs unless --available is set",
		"always includes cached metrics",
		"standardized to 0-100",
		"--available -P omlx",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("model list help missing %q:\n%s", want, out.String())
		}
	}
}

func TestModelsListSkipsProviderAPIsByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls++
	}))
	defer server.Close()

	configPath := writeModelsTestConfig(t, fmt.Sprintf(`{
		"default_provider":"local",
		"providers":{
			"local":{
				"base_url":%q,
				"model":"configured-model",
				"models":["second-model"],
				"enabled":true,
				"requires_key":false
			}
		}
	}`, server.URL))
	oldConfig := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(oldConfig) })

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"--config", configPath, "models", "list"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("instant model list called provider API %d time(s)", calls)
	}
	for _, want := range []string{"SCORE", "SPEED", "LATENCY", "local", "configured-model", "second-model"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("model list output missing %q:\n%s", want, out.String())
		}
	}
}

func TestModelsListAvailableCanTargetOneProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var omlxCalls, otherCalls int
	omlx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		omlxCalls++
		if r.URL.Path != "/v1/models" {
			t.Errorf("OMLX request path = %q, want /v1/models", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"local-a"},{"id":"local-b"}]}`)
	}))
	defer omlx.Close()
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		otherCalls++
	}))
	defer other.Close()

	configPath := writeModelsTestConfig(t, fmt.Sprintf(`{
		"default_provider":"omlx",
		"providers":{
			"omlx":{
				"base_url":%q,
				"model":"configured-omlx",
				"enabled":true,
				"requires_key":false
			},
			"other":{
				"base_url":%q,
				"model":"configured-other",
				"enabled":true,
				"requires_key":false
			}
		}
	}`, omlx.URL, other.URL))
	oldConfig := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(oldConfig) })

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{
		"--config", configPath, "models", "list", "--available", "-P", "omlx", "--timeout-seconds", "1",
	}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if omlxCalls != 1 {
		t.Fatalf("OMLX API calls = %d, want 1", omlxCalls)
	}
	if otherCalls != 0 {
		t.Fatalf("unselected provider API calls = %d, want 0", otherCalls)
	}
	for _, want := range []string{"omlx", "local-a", "local-b"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("targeted model list output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "configured-other") {
		t.Fatalf("targeted model list included unselected provider:\n%s", out.String())
	}
}

func TestFetchOpenAICompatibleModelsContextUsesSharedRequestSpec(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("request path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.llambo+json" {
			t.Errorf("Accept = %q, want extra-header override", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gateway-key" {
			t.Errorf("Authorization = %q, want extra-header override", got)
		}
		fmt.Fprint(w, `{"data":[{"id":"zeta","name":"Zeta"},{"id":"alpha","owned_by":"test"}]}`)
	}))
	defer server.Close()

	got := fetchOpenAICompatibleModelsContext(context.Background(), "test", providers.Config{
		BaseURL: server.URL,
		APIKey:  "source-key",
		ExtraHeaders: map[string]string{
			"Accept":        "application/vnd.llambo+json",
			"Authorization": "Bearer gateway-key",
		},
	}, 1)
	if want := []string{"alpha", "zeta"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("models = %v, want %v", got, want)
	}
}

func TestFetchOpenAICompatibleModelsContextRejectsBlankBaseURL(t *testing.T) {
	t.Parallel()

	if got := fetchOpenAICompatibleModelsContext(context.Background(), "test", providers.Config{APIKey: "key"}, 1); got != nil {
		t.Fatalf("models = %v, want nil for blank base URL", got)
	}
}

func TestFetchOpenAICompatibleModelsContextAcceptsMalformedOptionalMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"usable","architecture":"provider-specific"}]}`)
	}))
	defer server.Close()

	got := fetchOpenAICompatibleModelsContext(context.Background(), "test", providers.Config{BaseURL: server.URL}, 1)
	if want := []string{"usable"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("models = %v, want %v", got, want)
	}
}

func TestFetchGeminiModelsContextUsesTolerantSharedDecoder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("request path = %q, want /models", r.URL.Path)
		}
		if got := r.URL.Query().Get("key"); got != "source-key" {
			t.Errorf("key = %q, want source-key", got)
		}
		fmt.Fprint(w, `{
			"models": [
				{"name": "models/zeta", "displayName": 99},
				{"name": "models/models/alpha", "supportedGenerationMethods": "not-an-array"},
				{"name": "models/"}
			]
		}`)
	}))
	defer server.Close()

	got := fetchGeminiModelsContext(context.Background(), "test", providers.Config{
		BaseURL: server.URL,
		APIKey:  "source-key",
	}, 1)
	if want := []string{"models/alpha", "zeta"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("models = %v, want %v", got, want)
	}
}

func TestFetchModelListContextReturnsNilForNonSuccessStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	called := false
	decode := func([]byte) ([]string, error) {
		called = true
		return []string{"unexpected"}, nil
	}
	if got := fetchModelListContext(context.Background(), server.URL, nil, 1, decode); got != nil {
		t.Fatalf("models = %v, want nil", got)
	}
	if called {
		t.Fatal("decoder called for non-success response")
	}
}

func TestFetchModelListContextReturnsNilForDecodeFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{invalid`)
	}))
	defer server.Close()

	if got := fetchModelListContext(context.Background(), server.URL, nil, 1, catalog.DecodeOpenAICompatibleModelIDs); got != nil {
		t.Fatalf("models = %v, want nil", got)
	}
}

func TestModelsListMetricsUsesCatalogWithoutProviderCalls(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls++
	}))
	defer server.Close()

	configPath := writeModelsTestConfig(t, fmt.Sprintf(`{
		"default_provider":"local",
		"providers":{
			"local":{
				"base_url":%q,
				"model":"measured-model",
				"models":["unmeasured-model"],
				"enabled":true,
				"requires_key":false
			}
		}
	}`, server.URL))
	oldConfig := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(oldConfig) })
	now := time.Now().UTC()
	catPath := filepath.Join(home, ".config", "llambo", "catalog.json")
	if err := catalog.Save(catPath, &catalog.Catalog{
		Version: 1,
		Providers: map[string]*catalog.ProviderCatalog{
			"local": {
				Models: map[string]*catalog.ModelEntry{
					"measured-model": {
						Metadata: catalog.ModelMetadata{Pricing: catalog.ModelPricing{Prompt: "0.000001", Completion: "0.000002"}},
						Quality: map[string]catalog.QualityEvidence{
							"writing": {Score: 0.98, Source: "test", UpdatedAt: now},
						},
						Benchmarks: map[string]catalog.BenchmarkEvidence{
							"saved": {Score: 0.98, LatencyMS: 200, SpeedTokensPerSecond: 200, Source: "test", UpdatedAt: now},
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"--config", configPath, "models", "list"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("metrics model list called provider API %d time(s)", calls)
	}
	for _, want := range []string{"LLAMBO SCORE", "TASK SCORE", "SPEED", "LATENCY", "OUTPUT $/1M", "writing 98.0/100", "200.0 tok/s", "200ms", "unmeasured-model", "$2", "—"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("metrics model list output missing %q:\n%s", want, out.String())
		}
	}

	var csvOut bytes.Buffer
	if err := execute(context.Background(), []string{"--config", configPath, "models", "list", "--metrics", "--csv"}, &csvOut, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"provider,enabled,model,primary,llambo_score,llambo_provenance,task_score,speed,latency,output_cost_per_1m_usd", "writing 98.0/100", "$2"} {
		if !strings.Contains(csvOut.String(), want) {
			t.Errorf("metrics CSV output missing %q:\n%s", want, csvOut.String())
		}
	}
}

func TestModelsListIncludesCatalogOnlyScoredModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls++
	}))
	defer server.Close()

	configPath := writeModelsTestConfig(t, fmt.Sprintf(`{
		"default_provider":"local",
		"providers":{
			"local":{
				"base_url":%q,
				"model":"configured-model",
				"enabled":true,
				"requires_key":false
			}
		}
	}`, server.URL))
	oldConfig := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(oldConfig) })

	catPath := filepath.Join(home, ".config", "llambo", "catalog.json")
	if err := catalog.Save(catPath, &catalog.Catalog{
		Version: 1,
		Providers: map[string]*catalog.ProviderCatalog{
			"local": {
				Models: map[string]*catalog.ModelEntry{
					"catalog-scored-model": {
						Benchmarks: map[string]catalog.BenchmarkEvidence{
							"saved": {Score: 0.91, UpdatedAt: time.Now().UTC()},
						},
					},
					"catalog-unmeasured-model": {},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"--config", configPath, "models", "list"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("catalog-only model list called provider API %d time(s)", calls)
	}
	for _, want := range []string{"configured-model", "catalog-scored-model"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("catalog metric list output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "catalog-unmeasured-model") {
		t.Fatalf("catalog-only unmeasured model was unexpectedly listed:\n%s", out.String())
	}
}

func TestModelMetricLabelsDoesNotInferSpeedFromTotalLatency(t *testing.T) {
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"openai": {Models: map[string]*catalog.ModelEntry{
			"slow-looking": {
				LastPing: catalog.PingState{
					Success:   true,
					LatencyMS: 2000,
					TokensOut: 2,
				},
			},
			"measured": {
				LastPing: catalog.PingState{
					Success:              true,
					LatencyMS:            2000,
					TokensOut:            2,
					SpeedTokensPerSecond: 25,
				},
			},
		}},
	}}

	_, _, _, speed, latency := modelMetricLabels(cat, nil, "openai", "slow-looking")
	if speed != "—" || latency != "2000ms" {
		t.Fatalf("legacy speed fallback = %q with latency %q, want em dash and 2000ms", speed, latency)
	}
	_, _, _, speed, _ = modelMetricLabels(cat, nil, "openai", "measured")
	if speed != "25.0 tok/s" {
		t.Fatalf("persisted speed = %q, want 25.0 tok/s", speed)
	}
}

func TestModelMetricLabelsUsesOMLXSpeedEstimate(t *testing.T) {
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"omlx": {Models: map[string]*catalog.ModelEntry{
			"Qwen3.8-27B-4bit": {
				LastPing: catalog.PingState{
					Success:              true,
					SpeedTokensPerSecond: 12.5,
					CheckedAt:            time.Now().UTC(),
				},
			},
			"local": {
				LastPing: catalog.PingState{
					Success:              true,
					SpeedTokensPerSecond: 12.5,
					CheckedAt:            time.Now().UTC(),
				},
			},
		}},
	}}

	_, _, _, speed, _ := modelMetricLabels(cat, nil, "omlx", "Qwen3.8-27B-4bit")
	if speed != "~20.3 tok/s" {
		t.Fatalf("estimated speed = %q, want estimate instead of cached measurement", speed)
	}

	_, _, _, speed, _ = modelMetricLabels(cat, nil, "omlx", "local")
	if speed != "12.5 tok/s" {
		t.Fatalf("unestimatable speed = %q, want cached measurement to remain fallback", speed)
	}
}

func TestModelMetricLabelsPrefersFreshLiveTiming(t *testing.T) {
	benchmarkAt := time.Now().UTC().Add(-time.Hour)
	liveAt := benchmarkAt.Add(time.Minute)
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"omlx": {Models: map[string]*catalog.ModelEntry{
			"model": {
				Benchmarks: map[string]catalog.BenchmarkEvidence{
					"local": {LatencyMS: 215528, SpeedTokensPerSecond: 3, UpdatedAt: benchmarkAt},
				},
				LastPing: catalog.PingState{
					Success:              true,
					LatencyMS:            10156,
					SpeedTokensPerSecond: 12.5,
					CheckedAt:            liveAt,
				},
			},
		}},
	}}

	_, _, _, speed, latency := modelMetricLabels(cat, nil, "omlx", "model")
	if speed != "12.5 tok/s" || latency != "10156ms" {
		t.Fatalf("metrics = speed %q, latency %q, want 12.5 tok/s and 10156ms", speed, latency)
	}
}

func TestModelMetricLabelsSeparatesLlamboAndTaskScore(t *testing.T) {
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"omlx": {Models: map[string]*catalog.ModelEntry{
			"model": {Quality: map[string]catalog.QualityEvidence{"writing": {Score: .98}}},
		}},
	}}
	snapshot := &evals.OMLXScoreSnapshot{FormulaVersion: evals.CategoryFormulaVersion, PopulationFingerprint: "fingerprint", Inventory: []string{"model"}, CategoryScores: map[string]map[string]*evals.LlamboScore{"model": {"coding": {Score: 82.25, Coverage: .6, TrustedCoverage: .45, Confidence: "medium", WinnerStatus: "official"}}}}
	llambo, provenance, task, _, _ := modelMetricLabels(cat, snapshot, "omlx", "model")
	if llambo != "coding=82.2 (official)" || task != "writing 98.0/100" || !strings.Contains(provenance, "formula=LLAMBO-9-category") || !strings.Contains(provenance, "coding={coverage=0.600000,trusted_coverage=0.450000,confidence=medium,winner=official,stale=false}") || !strings.Contains(provenance, "writing=unresolved") {
		t.Fatalf("scores/provenance were incomplete: llambo=%q provenance=%q task=%q", llambo, provenance, task)
	}
	llambo, _, _, _, _ = modelMetricLabels(cat, snapshot, "hosted", "model")
	if llambo != "—" {
		t.Fatalf("hosted model got LLAMBO SCORE %q", llambo)
	}
}

func TestAppendCatalogMetricModelsOMLXUsesOnlySnapshotInventory(t *testing.T) {
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"omlx": {Models: map[string]*catalog.ModelEntry{"model-a": {Quality: map[string]catalog.QualityEvidence{"writing": {Score: .9}}}, "model-b": {}}},
	}}
	snapshot := &evals.OMLXScoreSnapshot{Inventory: []string{"model-b"}}
	if got := appendCatalogMetricModels(cat, snapshot, "omlx", nil); strings.Join(got, ",") != "model-b" {
		t.Fatalf("stale catalog model leaked into OMLX listing: %#v", got)
	}
	if got := appendCatalogMetricModels(cat, &evals.OMLXScoreSnapshot{Inventory: []string{}}, "omlx", nil); len(got) != 0 {
		t.Fatalf("empty snapshot retained stale OMLX models: %#v", got)
	}
}

func writeModelsTestConfig(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
