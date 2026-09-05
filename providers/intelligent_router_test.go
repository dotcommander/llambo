package providers

import (
	"testing"
	"time"
)

func TestScoreCandidates_RespectsDailyRequestLimit(t *testing.T) {
	t.Parallel()
	today := time.Now().UTC().Format("2006-01-02")
	configs := map[string]Config{
		"groq":   {Model: "openai/gpt-oss-120b", Quality: 0.7, ExpectedLatencyMs: 300},
		"openai": {Model: "gpt-5-mini", Quality: 0.7, ExpectedLatencyMs: 1200},
	}
	healthy := []Backend{{Name: "groq", Model: "openai/gpt-oss-120b"}, {Name: "openai", Model: "gpt-5-mini"}}
	routing := RoutingConfig{Mode: "balanced", DailyMaxRequests: map[string]int64{"groq": 1}}
	metrics := map[string]ProviderRuntimeMetrics{
		"groq": {DailyWindow: today, DailyRequests: 1},
	}

	got := scoreCandidates(configs, healthy, routing, metrics, IntentChat, 200)
	for _, c := range got {
		if c.Provider == "groq" {
			t.Fatalf("expected groq to be excluded by daily request limit")
		}
	}
}

func TestScoreCandidates_RespectsDailyTokenAndCostLimit(t *testing.T) {
	t.Parallel()
	today := time.Now().UTC().Format("2006-01-02")
	configs := map[string]Config{
		"a": {Model: "a", Quality: 0.7, InputCostPM: 2.0, OutputCostPM: 2.0},
		"b": {Model: "b", Quality: 0.7},
	}
	healthy := []Backend{{Name: "a", Model: "a"}, {Name: "b", Model: "b"}}
	routing := RoutingConfig{
		Mode:            "balanced",
		DailyMaxTokens:  map[string]int64{"a": 1000},
		DailyMaxCostUSD: map[string]float64{"a": 0.001},
	}
	metrics := map[string]ProviderRuntimeMetrics{
		"a": {DailyWindow: today, DailyTokens: 950, DailyCostUSD: 0.0009},
	}

	got := scoreCandidates(configs, healthy, routing, metrics, IntentChat, 200)
	for _, c := range got {
		if c.Provider == "a" {
			t.Fatalf("expected provider a to be excluded by daily token/cost limits")
		}
	}
}

func TestScoreCandidates_RespectsWildcardDailyLimits(t *testing.T) {
	t.Parallel()
	today := time.Now().UTC().Format("2006-01-02")
	configs := map[string]Config{"x": {Model: "x"}}
	healthy := []Backend{{Name: "x", Model: "x"}}
	routing := RoutingConfig{DailyMaxRequests: map[string]int64{"*": 2}}
	metrics := map[string]ProviderRuntimeMetrics{"x": {DailyWindow: today, DailyRequests: 2}}

	got := scoreCandidates(configs, healthy, routing, metrics, IntentChat, 100)
	if len(got) != 0 {
		t.Fatalf("expected no candidates due to wildcard daily request limit, got %d", len(got))
	}
}

func TestScoreCandidates_CatalogBackendMatchesBaseProviderFilters(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"edge-primary:vendor:model": {Model: "vendor:model", Quality: 0.8},
		"groq:llama":                {Model: "llama", Quality: 0.8},
	}
	healthy := []Backend{
		{Name: "edge-primary:vendor:model", Model: "vendor:model"},
		{Name: "groq:llama", Model: "llama"},
	}

	got := scoreCandidates(configs, healthy, RoutingConfig{AllowedProviders: []string{"edge-primary"}}, nil, IntentChat, 1000)
	if len(got) != 1 || got[0].Provider != "edge-primary:vendor:model" {
		t.Fatalf("expected base-provider allow rule to keep only edge-primary:vendor:model, got %#v", got)
	}

	got = scoreCandidates(configs, healthy, RoutingConfig{DeniedProviders: []string{"edge-primary"}}, nil, IntentChat, 1000)
	if len(got) != 1 || got[0].Provider != "groq:llama" {
		t.Fatalf("expected base-provider deny rule to exclude edge-primary:vendor:model, got %#v", got)
	}
}
