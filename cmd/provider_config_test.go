package cmd

import (
	"reflect"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/providers"
)

func TestSortedProviderNames(t *testing.T) {
	configs := map[string]providers.Config{
		"zai":    {Enabled: true},
		"openai": {Enabled: true},
		"groq":   {Enabled: true},
	}

	got := sortedProviderNames(configs)
	want := []string{"groq", "openai", "zai"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedProviderNames() mismatch\nwant=%v\ngot=%v", want, got)
	}
}

func TestEnabledProviderConfigs(t *testing.T) {
	configs := map[string]providers.Config{
		"openai": {Enabled: true, Model: "gpt-5-mini"},
		"groq":   {Enabled: true, Model: "llama"},
		"zai":    {Enabled: false, Model: "glm"},
	}

	got := enabledProviderConfigs(configs)
	if len(got) != 2 {
		t.Fatalf("expected 2 enabled providers, got %d", len(got))
	}
	if _, ok := got["zai"]; ok {
		t.Fatal("expected disabled provider to be excluded")
	}
	if got["openai"].Model != "gpt-5-mini" {
		t.Fatalf("expected openai config to be preserved, got %+v", got["openai"])
	}
	if got["groq"].Model != "llama" {
		t.Fatalf("expected groq config to be preserved, got %+v", got["groq"])
	}
	configs["openai"] = providers.Config{Enabled: true, Model: "changed"}
	if got["openai"].Model != "gpt-5-mini" {
		t.Fatalf("expected helper result to be independent from later map writes, got %+v", got["openai"])
	}
	got["new"] = providers.Config{Enabled: true, Model: "new-model"}
	if _, ok := configs["new"]; ok {
		t.Fatal("expected helper to return a distinct map")
	}
}

func TestRoutingProviderConfigsPinnedCatalogModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	now := time.Now().UTC()
	requireNoError(t, catalog.Save(catalogPathForHome(home), &catalog.Catalog{
		Version: 1,
		Providers: map[string]*catalog.ProviderCatalog{
			"openai": {
				Models: map[string]*catalog.ModelEntry{
					"gpt-5": {Pinned: true, FirstSeen: now, LastSeen: now},
					"gpt-5-mini": {
						Pinned:    true,
						FirstSeen: now,
						LastSeen:  now,
						Metadata: catalog.ModelMetadata{
							ContextLength:       200_000,
							SupportedParameters: []string{"tools", "response_format", "reasoning_effort"},
							Pricing: catalog.ModelPricing{
								Prompt:     "0.0000002",
								Completion: "0.0000008",
							},
						},
						Quality: map[string]catalog.QualityEvidence{
							"extraction": {Score: 0.91, Source: "distill"},
						},
					},
					"gpt-4o": {FirstSeen: now, LastSeen: now},
				},
			},
			"groq": {
				Models: map[string]*catalog.ModelEntry{
					"llama": {Pinned: true, Avoid: true, FirstSeen: now, LastSeen: now},
				},
			},
		},
	}))

	configs := map[string]providers.Config{
		"openai": {Enabled: true, Model: "configured", ProviderType: "openai", BaseURL: "https://api.openai.com"},
		"groq":   {Enabled: true, Model: "llama", ProviderType: "openai", BaseURL: "https://api.groq.com/openai"},
	}

	got, err := routingProviderConfigs(configs, providers.RoutingConfig{CatalogModels: "pinned"})
	requireNoError(t, err)

	if len(got) != 2 {
		t.Fatalf("expected 2 pinned backends, got %d: %#v", len(got), got)
	}
	if got["openai:gpt-5"].Model != "gpt-5" {
		t.Fatalf("expected gpt-5 backend, got %#v", got["openai:gpt-5"])
	}
	if got["openai:gpt-5-mini"].Model != "gpt-5-mini" {
		t.Fatalf("expected gpt-5-mini backend, got %#v", got["openai:gpt-5-mini"])
	}
	if got["openai:gpt-5-mini"].Quality != 0.91 {
		t.Fatalf("expected catalog quality to be applied, got %#v", got["openai:gpt-5-mini"])
	}
	if !reflect.DeepEqual(got["openai:gpt-5-mini"].Capabilities, []string{"extraction", "long_context", "reasoning", "structured_outputs", "tools"}) {
		t.Fatalf("expected catalog task capability, got %#v", got["openai:gpt-5-mini"].Capabilities)
	}
	if got["openai:gpt-5-mini"].InputCostPM < 0.199 || got["openai:gpt-5-mini"].InputCostPM > 0.201 ||
		got["openai:gpt-5-mini"].OutputCostPM < 0.799 || got["openai:gpt-5-mini"].OutputCostPM > 0.801 {
		t.Fatalf("expected metadata pricing to be applied, got %#v", got["openai:gpt-5-mini"])
	}
	if _, ok := got["openai:gpt-4o"]; ok {
		t.Fatal("expected unpinned model to be excluded")
	}
	if _, ok := got["groq:llama"]; ok {
		t.Fatal("expected avoided pinned model to be excluded")
	}
}

func TestRoutingProviderConfigsPinnedCatalogModelsRequiresPins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configs := map[string]providers.Config{
		"openai": {Enabled: true, Model: "configured", ProviderType: "openai"},
	}

	_, err := routingProviderConfigs(configs, providers.RoutingConfig{CatalogModels: "pinned"})
	if err == nil {
		t.Fatal("expected missing pinned catalog models to fail")
	}
}

func catalogPathForHome(home string) string {
	return home + "/.config/llambo/catalog.json"
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
