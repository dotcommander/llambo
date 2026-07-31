package cmd

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

func TestPingProviderUsesParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := pingProviderContext(ctx, "test", providers.Config{APIKey: "test", BaseURL: "http://127.0.0.1:1", Model: "test-model"}, "hello", time.Minute)
	if result.Error == "" || !strings.Contains(result.Error, "context canceled") {
		t.Fatalf("expected parent cancellation, got %#v", result)
	}
}

func restorePingFlags() func() {
	prevTimeout := pingTimeout
	return func() { pingTimeout = prevTimeout }
}

func TestValidatePingFlags(t *testing.T) {
	defer restorePingFlags()()
	pingTimeout = 0

	err := validatePingFlags()
	if err == nil || !strings.Contains(err.Error(), "--timeout-seconds") {
		t.Fatalf("expected invalid timeout error, got %v", err)
	}
}

func TestRecordPingRoutingMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routing-metrics.json")
	results := []PingResult{
		{
			Provider:  "openai",
			Model:     "gpt-5",
			Success:   true,
			Latency:   1500 * time.Millisecond,
			Response:  "ok",
			TokensIn:  10,
			TokensOut: 5,
		},
		{
			Provider: "groq",
			Model:    "llama",
			Success:  false,
			Error:    "timeout",
			Latency:  2 * time.Second,
		},
	}

	err := recordPingRoutingMetrics(results, providers.RoutingConfig{
		MetricsPath:   path,
		CatalogModels: "pinned",
	})
	if err != nil {
		t.Fatal(err)
	}

	store, err := providers.NewRoutingMetricsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()

	if snapshot["openai"].Successes != 1 || snapshot["openai"].TotalTokens != 15 {
		t.Fatalf("expected provider-level success metrics, got %#v", snapshot["openai"])
	}
	if snapshot["openai:gpt-5"].Successes != 1 || snapshot["openai:gpt-5"].TotalTokens != 15 {
		t.Fatalf("expected catalog backend success metrics, got %#v", snapshot["openai:gpt-5"])
	}
	if snapshot["groq"].Failures != 1 || snapshot["groq"].Timeouts != 1 {
		t.Fatalf("expected provider-level failure metrics, got %#v", snapshot["groq"])
	}
	if snapshot["groq:llama"].Failures != 1 || snapshot["groq:llama"].Timeouts != 1 {
		t.Fatalf("expected catalog backend failure metrics, got %#v", snapshot["groq:llama"])
	}
}

func TestRunOrderedProviderGroupsPreservesProviderOrder(t *testing.T) {
	t.Parallel()
	type target struct {
		provider string
		model    string
	}
	targets := []target{{"a", "a1"}, {"b", "b1"}, {"a", "a2"}}
	started := make(chan string, len(targets))
	results := runOrderedProviderGroups(targets, func(target target) string { return target.provider }, func(_ int, target target) string {
		started <- target.model
		return target.model
	})
	if got, want := results, []string{"a1", "b1", "a2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %v, want %v", got, want)
	}
	seen := make([]string, 0, len(targets))
	for range targets {
		seen = append(seen, <-started)
	}
	firstA, secondA := -1, -1
	for index, model := range seen {
		if model == "a1" {
			firstA = index
		}
		if model == "a2" {
			secondA = index
		}
	}
	if firstA < 0 || secondA < 0 || firstA >= secondA {
		t.Fatalf("same-provider execution order = %v, want a1 before a2", seen)
	}
}

func TestModelVariants_DedupAndOrder(t *testing.T) {
	cfg := providers.Config{
		Model:  "gpt-4o-mini",
		Models: []string{"gpt-4.1-mini", "gpt-4o-mini", "", "gpt-5-mini"},
	}

	got := modelVariants(cfg)
	want := []string{"gpt-4o-mini", "gpt-4.1-mini", "gpt-5-mini"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("modelVariants() mismatch\nwant=%v\ngot=%v", want, got)
	}
}

func TestBuildBenchmarkTargets_MultiModelPerProvider(t *testing.T) {
	configs := map[string]providers.Config{
		"openai": {
			Enabled:  true,
			Priority: 1,
			Model:    "gpt-4o-mini",
			Models:   []string{"gpt-5-mini", "gpt-4o-mini"},
		},
		"groq": {
			Enabled:  true,
			Priority: 2,
			Model:    "openai/gpt-oss-120b",
		},
		"disabled": {
			Enabled:  false,
			Priority: 1,
			Model:    "none",
			Models:   []string{"none-2"},
		},
	}

	targets := buildPingTargets(configs, map[string]costs.ModelCost{
		"openai:gpt-4o-mini": {InputPer1M: 0, OutputPer1M: 0, InputExplicit: true, OutputExplicit: true},
		"openai:gpt-5-mini":  {InputPer1M: 1, OutputPer1M: 2, InputExplicit: true, OutputExplicit: true},
	}, providers.Blocklist{}, 0, false, nil)
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}

	got := []string{
		targets[0].Name + ":" + targets[0].Config.Model,
		targets[1].Name + ":" + targets[1].Config.Model,
		targets[2].Name + ":" + targets[2].Config.Model,
	}
	want := []string{
		"openai:gpt-4o-mini",
		"openai:gpt-5-mini",
		"groq:openai/gpt-oss-120b",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target list mismatch\nwant=%v\ngot=%v", want, got)
	}
	if targets[0].CostStatus != "free" {
		t.Fatalf("expected first target to be free, got %s", targets[0].CostStatus)
	}
	if targets[1].CostStatus != "paid" {
		t.Fatalf("expected second target to be paid, got %s", targets[1].CostStatus)
	}
}

func TestBuildPingTargets_Blocklist(t *testing.T) {
	configs := map[string]providers.Config{
		"openai": {Enabled: true, Priority: 1, Model: "gpt-4o-mini", Models: []string{"o3-mini"}},
	}
	targets := buildPingTargets(configs, nil, providers.NewBlocklist([]string{"OpenAI:o3-mini"}), 0, false, nil)
	for _, tt := range targets {
		if tt.Config.Model == "o3-mini" {
			t.Fatalf("blocklisted model o3-mini must not be a ping target")
		}
	}
	if len(targets) != 1 || targets[0].Config.Model != "gpt-4o-mini" {
		t.Fatalf("expected only gpt-4o-mini, got %#v", targets)
	}
}

func TestBuildPingTargets_SkipsGeminiNonTextChatModels(t *testing.T) {
	configs := map[string]providers.Config{
		"gemini": {
			Enabled:      true,
			Priority:     1,
			ProviderType: "gemini",
			Model:        "gemini-2.5-flash",
			Models: []string{
				"gemini-embedding-001",
				"gemini-2.5-flash-image",
				"lyria-3-pro-preview",
				"gemini-2.5-pro-preview-tts",
				"veo-3.1-generate-preview",
			},
		},
	}
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"gemini": {Models: map[string]*catalog.ModelEntry{
			"gemini-2.5-flash": {
				Metadata: catalog.ModelMetadata{SupportedGenerationMethods: []string{"generateContent", "countTokens"}},
			},
			"gemini-embedding-001": {
				Metadata: catalog.ModelMetadata{SupportedGenerationMethods: []string{"embedContent", "countTokens"}},
			},
		}},
	}}

	targets := buildPingTargets(configs, nil, providers.Blocklist{}, 0, false, cat)
	if len(targets) != 1 {
		t.Fatalf("expected only text chat target, got %#v", targets)
	}
	if got := targets[0].Config.Model; got != "gemini-2.5-flash" {
		t.Fatalf("target model = %q, want gemini-2.5-flash", got)
	}
}

func TestFilterByProvider(t *testing.T) {
	targets := []pingTarget{
		{Name: "nvidia", Config: providers.Config{Model: "nemotron-3"}},
		{Name: "nvidia", Config: providers.Config{Model: "llama-70b"}},
		{Name: "groq", Config: providers.Config{Model: "gpt-oss-120b"}},
		{Name: "openai", Config: providers.Config{Model: "gpt-5-mini"}},
	}

	tests := []struct {
		name   string
		filter string
		want   int
	}{
		{"empty filter returns all", "", 4},
		{"single provider", "nvidia", 2},
		{"multiple comma-separated", "nvidia,groq", 3},
		{"case insensitive", "NVIDIA", 2},
		{"mixed case comma-separated", "Nvidia, Groq", 3},
		{"nonexistent provider", "azure", 0},
		{"whitespace around commas", " nvidia , groq ", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByProvider(targets, tt.filter)
			if len(got) != tt.want {
				t.Fatalf("filterByProvider(%q) returned %d targets, want %d", tt.filter, len(got), tt.want)
			}
		})
	}
}
