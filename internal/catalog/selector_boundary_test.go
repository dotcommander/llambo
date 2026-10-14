package catalog

import (
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/stretchr/testify/require"
)

func TestResolveModelsProviderFilterIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	cat := testSelectorCatalog()

	got, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: "all", ProviderFilter: "OPENAI"})
	require.NoError(t, err)
	require.NotEmpty(t, got)

	_, err = ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: "all", ProviderFilter: "anthropic"})
	require.ErrorContains(t, err, `no models match selector "all"`)
}

func TestResolveModelsAvoidExcludesEvenWithQuarantineIncluded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	cat := testSelectorCatalog()
	cat.Providers["openai"].Models["avoided"] = &ModelEntry{Avoid: true, QuarantineUntil: now.Add(time.Hour)}
	cfgs := testSelectorConfigs()
	cfg := cfgs["openai"]
	cfg.Models = []string{"avoided"}
	cfgs["openai"] = cfg

	got, err := ResolveModels(cat, cfgs, nil, SelectorOptions{Selector: "all", IncludeQuarantine: true, Now: now})
	require.NoError(t, err)
	require.NotContains(t, modelNames(got), "avoided")

	cat.Providers["openai"].Models["avoided"].Avoid = false
	unavoided, err := ResolveModels(cat, cfgs, nil, SelectorOptions{Selector: "all", IncludeQuarantine: true, Now: now})
	require.NoError(t, err)
	require.Contains(t, modelNames(unavoided), "avoided")
}

func TestResolveModelsPropagatesQuarantineUntil(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	cat := testSelectorCatalog()
	cat.Providers["openai"].Models["free"].QuarantineUntil = now.Add(30 * time.Minute)
	costMap := map[string]costs.ModelCost{
		"openai:free": {InputPer1M: 0, OutputPer1M: 0, InputExplicit: true, OutputExplicit: true},
	}

	got, err := ResolveModels(cat, testSelectorConfigs(), costMap, SelectorOptions{Selector: "free", IncludeQuarantine: true, Now: now})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, now.Add(30*time.Minute), got[0].QuarantinedUntil)
}

func TestResolveModelsEmptySelectorDefaultsToConfiguredModels(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	cat := testSelectorCatalog()
	cat.Providers["openai"].Models["catalog-only"] = &ModelEntry{FirstSeen: now, LastSeen: now}

	got, err := ResolveModels(cat, testSelectorConfigs(), nil, SelectorOptions{Selector: "   ", Now: now})
	require.NoError(t, err)
	require.Equal(t, []string{"free"}, modelNames(got))
}
