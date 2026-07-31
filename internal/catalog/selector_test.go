package catalog

import (
	"errors"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
	"github.com/stretchr/testify/require"
)

func TestResolveModelsFreeRequiresExplicitZeroPrice(t *testing.T) {
	t.Parallel()
	cat := testSelectorCatalog()
	costMap := map[string]costs.ModelCost{
		"openai:free":        {InputPer1M: 0, OutputPer1M: 0, InputExplicit: true, OutputExplicit: true},
		"openai:unknown-row": {},
		"openai:paid":        {InputPer1M: 1, OutputPer1M: 2, InputExplicit: true, OutputExplicit: true},
	}

	got, err := ResolveModels(cat, testSelectorConfigs(), costMap, SelectorOptions{Selector: "free"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "free", got[0].Model)
	require.Equal(t, CostFree, got[0].CostStatus)
}

func TestResolveModelsUsesMetadataPricingWhenCostCSVAbsent(t *testing.T) {
	t.Parallel()
	cat := testSelectorCatalog()
	cat.Providers["openai"].Models["free"].Metadata.Pricing = ModelPricing{
		Prompt:     "0",
		Completion: "0",
	}
	cat.Providers["openai"].Models["paid"].Metadata.Pricing = ModelPricing{
		Prompt:     "0.0000005",
		Completion: "0.0000015",
	}

	free, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: "free"})
	require.NoError(t, err)
	require.Len(t, free, 1)
	require.Equal(t, "free", free[0].Model)
	require.Equal(t, CostFree, free[0].CostStatus)

	paid, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: "openai/paid"})
	require.NoError(t, err)
	require.Len(t, paid, 1)
	require.Equal(t, CostPaid, paid[0].CostStatus)
	require.Equal(t, 0.5, paid[0].InputPer1M)
	require.Equal(t, 1.5, paid[0].OutputPer1M)
}

func TestResolveModelsBlocklistExcludes(t *testing.T) {
	t.Parallel()
	cat := testSelectorCatalog()

	got, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{
		Selector:  "all",
		Blocklist: providers.NewBlocklist([]string{"OpenAI:FREE"}),
	})
	require.NoError(t, err)
	names := modelNames(got)
	require.NotContains(t, names, "free")
	require.Contains(t, names, "paid")
}

func TestResolveModelsTagCategoryAndQuarantine(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	cat := testSelectorCatalog()
	AddTag(cat.Providers["openai"].Models["smart"], "smart")
	cat.Providers["openai"].Models["speed"].LastPing = PingState{Success: true, LatencyMS: 400, CheckedAt: now}
	cat.Providers["openai"].Models["slow"].LastPing = PingState{Success: true, LatencyMS: int64((6 * time.Second).Milliseconds()), CheckedAt: now}
	cat.Providers["openai"].Models["quarantined"].QuarantineUntil = now.Add(time.Hour)

	tagged, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: "tag:smart", Now: now})
	require.NoError(t, err)
	require.Len(t, tagged, 1)
	require.Equal(t, "smart", tagged[0].Model)

	speed, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: "category:speed", Now: now})
	require.NoError(t, err)
	require.Len(t, speed, 1)
	require.Equal(t, "speed", speed[0].Model)

	all, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: "all", Now: now})
	require.NoError(t, err)
	for _, target := range all {
		require.NotEqual(t, "quarantined", target.Model)
	}

	withQuarantine, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: "all", IncludeQuarantine: true, Now: now})
	require.NoError(t, err)
	require.Contains(t, modelNames(withQuarantine), "quarantined")
}

func TestResolveModelsMetadataCategories(t *testing.T) {
	t.Parallel()
	cat := testSelectorCatalog()
	cat.Providers["openai"].Models["long"] = &ModelEntry{
		Metadata: ModelMetadata{TopProvider: ModelTopProvider{ContextLength: 200_000}},
	}
	cat.Providers["openai"].Models["tools"] = &ModelEntry{
		Metadata: ModelMetadata{SupportedParameters: []string{"tools", "temperature"}},
	}
	cat.Providers["openai"].Models["structured"] = &ModelEntry{
		Metadata: ModelMetadata{SupportedParameters: []string{"response_format"}},
	}
	cat.Providers["openai"].Models["thinking"] = &ModelEntry{
		Metadata: ModelMetadata{Reasoning: map[string]any{"default_enabled": true}},
	}

	tests := []struct {
		selector string
		model    string
	}{
		{"category:long_context", "long"},
		{"category:tools", "tools"},
		{"category:structured_outputs", "structured"},
		{"category:reasoning", "thinking"},
	}

	for _, tt := range tests {
		t.Run(tt.selector, func(t *testing.T) {
			got, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: tt.selector})
			require.NoError(t, err)
			require.Contains(t, modelNames(got), tt.model)
		})
	}
}

func TestResolveModelsDirectProviderModelSelector(t *testing.T) {
	t.Parallel()
	cat := testSelectorCatalog()
	cat.Providers["openai"].Models["GLM-5.2"] = &ModelEntry{}
	cfgs := testSelectorConfigs()
	cfg := cfgs["openai"]
	cfg.Model = "GLM-5.2"
	cfg.Models = []string{"fallback"}
	cfgs["openai"] = cfg

	got, err := ResolveModels(cat, cfgs, nil, SelectorOptions{Selector: "openai/glm-5.2"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "openai", got[0].Provider)
	require.Equal(t, "GLM-5.2", got[0].Model)
	require.Equal(t, "GLM-5.2", got[0].Config.Model)
}

func TestRecordPingFailureAndSlowQuarantine(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	cat := testSelectorCatalog()

	RecordPing(cat, "openai", "free", false, time.Second, 0, 0, "timeout", now)
	RecordPing(cat, "openai", "free", false, time.Second, 0, 0, "timeout", now.Add(time.Minute))
	require.True(t, cat.Providers["openai"].Models["free"].QuarantineUntil.IsZero())

	RecordPing(cat, "openai", "free", false, time.Second, 0, 0, "timeout", now.Add(2*time.Minute))
	entry := cat.Providers["openai"].Models["free"]
	require.Equal(t, 3, entry.FailureCount)
	require.Equal(t, now.Add(2*time.Minute).Add(QuarantineDuration), entry.QuarantineUntil)
	require.Equal(t, "timeout", entry.LastPing.ErrorCategory)

	RecordPing(cat, "openai", "smart", true, 6*time.Second, 1, 1, "", now)
	require.Equal(t, now.Add(QuarantineDuration), cat.Providers["openai"].Models["smart"].QuarantineUntil)

	RecordPing(cat, "openai", "free", true, time.Second, 1, 1, "", now.Add(3*time.Minute))
	require.Equal(t, 0, entry.FailureCount)
	require.True(t, entry.QuarantineUntil.IsZero())
}

func TestClassifyError(t *testing.T) {
	t.Parallel()
	require.Equal(t, "model_unsupported", ClassifyError("invalid model specified"))
	require.Equal(t, "model_unsupported", ClassifyError("410 Gone: model reached end of life and is no longer available"))
	require.Equal(t, "rate_limit", ClassifyError("429 rate limit"))
	require.Equal(t, "auth", ClassifyError("unauthorized API key"))
	require.Equal(t, "timeout", ClassifyError(errors.New("context deadline exceeded").Error()))
}

func testSelectorCatalog() *Catalog {
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	return &Catalog{
		Version: 1,
		Providers: map[string]*ProviderCatalog{
			"openai": {
				Models: map[string]*ModelEntry{
					"free":        {FirstSeen: now, LastSeen: now},
					"unknown-row": {FirstSeen: now, LastSeen: now},
					"paid":        {FirstSeen: now, LastSeen: now},
					"smart":       {FirstSeen: now, LastSeen: now},
					"speed":       {FirstSeen: now, LastSeen: now},
					"slow":        {FirstSeen: now, LastSeen: now},
					"quarantined": {FirstSeen: now, LastSeen: now},
				},
			},
		},
	}
}

func testSelectorConfigs() map[string]providers.Config {
	return map[string]providers.Config{
		"openai": {Enabled: true, Model: "free", Priority: 1},
	}
}

func modelNames(targets []ModelTarget) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Model)
	}
	return names
}
