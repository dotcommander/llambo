package catalog

import (
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
	"github.com/stretchr/testify/require"
)

func TestResolveModelsCategoryPolicyCandidateScope(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cat := &Catalog{Providers: map[string]*ProviderCatalog{
		"alpha": {Models: map[string]*ModelEntry{
			"catalog-only": {},
			"measured": {Benchmarks: map[string]BenchmarkEvidence{
				"benchmark": {SpeedTokensPerSecond: 10},
			}},
			"primary": {},
			"tagged":  {Tags: []string{"custom"}},
		}},
	}}
	cfgs := map[string]providers.Config{
		"alpha": {Enabled: true, Model: "primary", Models: []string{"configured-only", "measured"}},
	}

	tests := []struct {
		name     string
		selector string
		want     []string
	}{
		{
			name:     "missing speed uses configured candidates and handles nil catalog entries",
			selector: "category:missing_speed",
			want:     []string{"configured-only", "primary"},
		},
		{
			name:     "missing speed hyphen alias uses configured candidates",
			selector: "category:missing-speed",
			want:     []string{"configured-only", "primary"},
		},
		{
			name:     "category whitespace unions catalog and configured candidates",
			selector: "category: missing_speed",
			want:     []string{"catalog-only", "configured-only", "primary", "tagged"},
		},
		{
			name:     "unknown categories retain tag fallback",
			selector: "category:custom",
			want:     []string{"tagged"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveModels(cat, cfgs, nil, SelectorOptions{Selector: tt.selector, Now: now})
			require.NoError(t, err)
			require.Equal(t, tt.want, modelNames(got))
		})
	}
}

func TestResolveModelsCategoryPolicyDirectSelectorPrecedence(t *testing.T) {
	t.Parallel()
	cfgs := map[string]providers.Config{
		"category:custom": {Enabled: true, Model: "ignored"},
	}

	got, err := ResolveModels(nil, cfgs, nil, SelectorOptions{Selector: "category:custom/ignored"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "category:custom", got[0].Provider)
	require.Equal(t, "ignored", got[0].Model)
}

func TestResolveModelsCategoryPolicyFiltersMetadata(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cat := &Catalog{Providers: map[string]*ProviderCatalog{
		"alpha": {Models: map[string]*ModelEntry{
			"fast":                {LastPing: PingState{Success: true, LatencyMS: 5_000}},
			"fast-quarantined":    {LastPing: PingState{Success: true, LatencyMS: 1}, QuarantineUntil: now.Add(time.Hour)},
			"fallback":            {Metadata: ModelMetadata{ContextLength: 128_000}},
			"healthy":             {LastPing: PingState{Success: true}},
			"healthy-quarantined": {LastPing: PingState{Success: true}, QuarantineUntil: now.Add(time.Hour)},
			"slow":                {LastPing: PingState{Success: true, LatencyMS: 5_001}},
			"threshold":           {Metadata: ModelMetadata{TopProvider: ModelTopProvider{ContextLength: 128_000}}},
			"top-provider-wins": {Metadata: ModelMetadata{
				ContextLength: 200_000,
				TopProvider:   ModelTopProvider{ContextLength: 127_999},
			}},
		}},
	}}
	cfgs := map[string]providers.Config{"alpha": {Enabled: true, Model: "healthy"}}

	tests := []struct {
		name     string
		selector string
		want     []string
	}{
		{name: "healthy excludes quarantine even when included", selector: "category:healthy", want: []string{"fast", "healthy", "slow"}},
		{name: "speed uses the five second boundary and excludes quarantine", selector: "category:speed", want: []string{"fast"}},
		{name: "long context honors threshold and top provider precedence", selector: "category:long_context", want: []string{"fallback", "threshold"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveModels(cat, cfgs, nil, SelectorOptions{
				Selector:          tt.selector,
				IncludeQuarantine: true,
				Now:               now,
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, modelNames(got))
		})
	}
}
