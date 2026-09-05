package cmd

import (
	"strings"
	"testing"

	"github.com/dotcommander/llambo/providers"
)

func mustDefaultRoutePreferences(t *testing.T) routePreferenceSet {
	t.Helper()
	prefs, err := parseRoutePreferences(defaultRoutePreferencesYAML)
	if err != nil {
		t.Fatalf("parse default route preferences: %v", err)
	}
	return prefs
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" fastest, cheapest ,balanced,,quality ")
	want := []string{"fastest", "cheapest", "balanced", "quality"}
	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %q at %d, got %q", want[i], i, got[i])
		}
	}
}

func TestPct(t *testing.T) {
	if pct(1, 4) != 25 {
		t.Fatalf("expected 25, got %v", pct(1, 4))
	}
	if pct(1, 0) != 0 {
		t.Fatalf("expected 0 for denominator 0, got %v", pct(1, 0))
	}
}

func TestFormatProviderCounts(t *testing.T) {
	counts := map[string]int{"b": 1, "a": 3}
	out := formatProviderCounts(counts, 4)
	if out != "a=3(75.0%), b=1(25.0%)" {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestCalculateActualBaseline(t *testing.T) {
	events := []providers.RouteEvent{
		{ChosenProvider: "openai", LatencyMs: 100, CostUSD: 0.01},
		{ChosenProvider: "openrouter", LatencyMs: 200, CostUSD: 0.02},
		{ChosenProvider: "", LatencyMs: 300, CostUSD: 0.03}, // ignored
	}
	b := calculateActualBaseline(events)
	if b.decisions != 2 {
		t.Fatalf("expected 2 decisions, got %d", b.decisions)
	}
	if b.totalLatencyMs != 300 {
		t.Fatalf("expected 300 latency total, got %d", b.totalLatencyMs)
	}
	if b.totalCost != 0.03 {
		t.Fatalf("expected 0.03 total cost, got %f", b.totalCost)
	}
}

func TestRouteQueryPrefersGeminiLiteForAtomicFacts(t *testing.T) {
	configs := map[string]providers.Config{
		"openai": {
			Model:        "gpt-5-mini",
			Enabled:      true,
			Capabilities: []string{"extraction"},
			Quality:      0.99,
		},
		"gemini": {
			Model:        "gemini-2.5-flash-lite",
			Enabled:      true,
			Capabilities: []string{"extraction"},
			Quality:      0.80,
		},
	}

	got, err := routeQuery("extract all atomic facts", configs, providers.RoutingConfig{}, nil, mustDefaultRoutePreferences(t))
	if err != nil {
		t.Fatalf("routeQuery returned error: %v", err)
	}
	if got.Intent != providers.IntentExtraction {
		t.Fatalf("expected extraction intent, got %s", got.Intent)
	}
	if got.Provider != "gemini" || got.Model != "gemini-2.5-flash-lite" {
		t.Fatalf("expected gemini/gemini-2.5-flash-lite, got %s/%s", got.Provider, got.Model)
	}
}

func TestRouteQueryPrefersDeepSeekForDraftWriting(t *testing.T) {
	configs := map[string]providers.Config{
		"openai": {
			Model:        "gpt-5-mini",
			Enabled:      true,
			Capabilities: []string{"chat"},
			Quality:      0.99,
		},
		"deepseek": {
			Model:        "deepseek-v4-pro",
			Enabled:      true,
			Capabilities: []string{"chat"},
			Quality:      0.70,
		},
	}

	got, err := routeQuery("write a first draft", configs, providers.RoutingConfig{}, nil, mustDefaultRoutePreferences(t))
	if err != nil {
		t.Fatalf("routeQuery returned error: %v", err)
	}
	if got.Provider != "deepseek" || got.Model != "deepseek-v4-pro" {
		t.Fatalf("expected deepseek/deepseek-v4-pro, got %s/%s", got.Provider, got.Model)
	}
}

func TestRouteQueryFallsBackToQualityRouter(t *testing.T) {
	configs := map[string]providers.Config{
		"openai": {
			Model:        "gpt-5-mini",
			Enabled:      true,
			Capabilities: []string{"extraction"},
			Quality:      0.90,
		},
		"gemini": {
			Model:        "gemini-2.0-flash",
			Enabled:      true,
			Capabilities: []string{"chat"},
			Quality:      0.60,
		},
	}

	prefs := mustDefaultRoutePreferences(t)
	prefs.FallbackDecider = routeModelChoice{}

	got, err := routeQuery("extract section headings", configs, providers.RoutingConfig{}, nil, prefs)
	if err != nil {
		t.Fatalf("routeQuery returned error: %v", err)
	}
	if got.Provider != "openai" || got.Model != "gpt-5-mini" {
		t.Fatalf("expected openai/gpt-5-mini, got %s/%s", got.Provider, got.Model)
	}
}

func TestRouteQueryProjectsColonBearingModelBackend(t *testing.T) {
	configs := map[string]providers.Config{
		"edge-primary:vendor:model": {
			Model:        "vendor:model",
			Enabled:      true,
			Capabilities: []string{"chat"},
			Quality:      0.90,
		},
	}

	got, err := routeQuery("choose a model", configs, providers.RoutingConfig{}, nil, routePreferenceSet{})
	if err != nil {
		t.Fatalf("routeQuery returned error: %v", err)
	}
	if got.Provider != "edge-primary" || got.Model != "vendor:model" {
		t.Fatalf("expected edge-primary/vendor:model, got %s/%s", got.Provider, got.Model)
	}
}

func TestFindConfiguredRouteModelProjectsColonBearingModelBackend(t *testing.T) {
	configs := map[string]providers.Config{
		"edge-primary:vendor:model": {Model: "vendor:model"},
	}

	provider, model := findConfiguredRouteModel(configs, "edge-primary", "vendor:model")
	if provider != "edge-primary" || model != "vendor:model" {
		t.Fatalf("expected edge-primary/vendor:model, got %s/%s", provider, model)
	}
}

func TestRouteQueryUsesGeminiLiteAsFallbackDecider(t *testing.T) {
	configs := map[string]providers.Config{
		"openai": {
			Model:        "gpt-5-mini",
			Enabled:      true,
			Capabilities: []string{"chat"},
			Quality:      0.99,
		},
		"gemini": {
			Model:        "gemini-2.5-flash-lite",
			Enabled:      true,
			Capabilities: []string{"chat"},
			Quality:      0.70,
		},
	}

	got, err := routeQuery("pick the best specialist for this task", configs, providers.RoutingConfig{}, nil, mustDefaultRoutePreferences(t))
	if err != nil {
		t.Fatalf("routeQuery returned error: %v", err)
	}
	if got.Provider != "gemini" || got.Model != "gemini-2.5-flash-lite" {
		t.Fatalf("expected gemini/gemini-2.5-flash-lite, got %s/%s", got.Provider, got.Model)
	}
	if !strings.Contains(got.Reason, "fallback route decider") {
		t.Fatalf("expected fallback-decider reason, got %q", got.Reason)
	}
}

func TestParseRoutePreferences(t *testing.T) {
	prefs, err := parseRoutePreferences([]byte(`
route_preferences:
  - name: reviewer
    match:
      any_phrases: ["review"]
    choose:
      provider: openai
      model: gpt-5-mini
    reason: review preference
fallback_decider:
  provider: gemini
  model: gemini-2.5-flash-lite
fallback_mode: fastest
`))
	if err != nil {
		t.Fatalf("parseRoutePreferences returned error: %v", err)
	}
	if len(prefs.Preferences) != 1 {
		t.Fatalf("expected 1 preference, got %d", len(prefs.Preferences))
	}
	if prefs.FallbackMode != string(providers.RoutingModeFastest) {
		t.Fatalf("expected fastest fallback mode, got %q", prefs.FallbackMode)
	}
}

func TestBuildRouteSimulationReport(t *testing.T) {
	baseline := actualBaseline{
		decisions:      2,
		totalLatencyMs: 300,
		totalCost:      0.03,
		providerCounts: map[string]int{"openai": 1, "openrouter": 1},
	}
	reports := []modeReport{{
		Mode: "balanced",
		Stats: simulationStats{
			decisions:      2,
			diffCount:      1,
			totalLatencyMs: 250,
			totalCost:      0.02,
			providerCounts: map[string]int{"openai": 2},
			sampleReasons:  []string{"openai/gpt: balanced | quality=0.90"},
		},
	}}

	md := buildRouteSimulationReport("/tmp/report.md", "/tmp/events.jsonl", 2, baseline, reports)
	checks := []string{
		"# Routing Simulation Report",
		"## Actual Baseline",
		"## Mode Comparison",
		"Sample Reason",
		"balanced \\| quality=0.90",
		"| balanced |",
		"/tmp/events.jsonl",
	}
	for _, c := range checks {
		if !strings.Contains(md, c) {
			t.Fatalf("expected report to contain %q\n%s", c, md)
		}
	}
}
