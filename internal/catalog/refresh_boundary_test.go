package catalog

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCachedRefreshResultTTLBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	lastRefresh := now.Add(-time.Hour)
	cat := &Catalog{Providers: map[string]*ProviderCatalog{
		"openai": {
			LastRefresh:  lastRefresh,
			EndpointUsed: "https://api.example/v1/models",
			Models: map[string]*ModelEntry{
				"fresh": {LastSeen: lastRefresh.Add(time.Minute)},
				"stale": {LastSeen: lastRefresh.Add(-2 * time.Hour)},
			},
		},
	}}
	opts := RefreshOptions{TTL: time.Hour}

	// Exactly at the TTL boundary the cache is expired: the rule is strictly
	// less than TTL.
	if _, cached := cachedRefreshResult(cat, "openai", opts, now); cached {
		t.Fatal("a refresh exactly one TTL ago must not be served from cache")
	}

	// One second inside the TTL serves only models seen since that refresh.
	result, cached := cachedRefreshResult(cat, "openai", opts, now.Add(-time.Second))
	require.True(t, cached)
	require.Equal(t, 1, result.Total)
	require.Equal(t, 1, result.UnchangedN)
	require.Equal(t, 0, result.UpdatedN)
	require.True(t, result.Cached)
	require.Equal(t, "https://api.example/v1/models", result.Endpoint)
	require.Equal(t, lastRefresh, result.LastRefresh)

	// Force, zero TTL, unknown provider, and future LastRefresh never cache.
	if _, cached := cachedRefreshResult(cat, "openai", RefreshOptions{TTL: time.Hour, Force: true}, now.Add(-time.Second)); cached {
		t.Fatal("Force must bypass the TTL cache")
	}
	if _, cached := cachedRefreshResult(cat, "openai", RefreshOptions{}, now.Add(-time.Second)); cached {
		t.Fatal("zero TTL must never serve from cache")
	}
	if _, cached := cachedRefreshResult(cat, "absent", opts, now.Add(-time.Second)); cached {
		t.Fatal("missing provider must not be served from cache")
	}
	future := &Catalog{Providers: map[string]*ProviderCatalog{
		"openai": {LastRefresh: now.Add(time.Hour), Models: map[string]*ModelEntry{}},
	}}
	if _, cached := cachedRefreshResult(future, "openai", opts, now); cached {
		t.Fatal("future LastRefresh must not be served from cache")
	}
}

func TestMergeRefreshCountersStaleAndTouch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cat := &Catalog{Providers: map[string]*ProviderCatalog{
		"openai": {Models: map[string]*ModelEntry{
			"same": {OwnedBy: "openai", FirstSeen: now, LastSeen: now},
		}},
	}}
	fetch := refreshFetch{
		result:   RefreshResult{Provider: "openai", Endpoint: "https://api.example/v1/models"},
		upstream: []UpstreamModel{{ID: "new"}, {ID: "same", OwnedBy: "openai"}},
	}

	result := mergeRefresh(cat, fetch, now)
	require.Equal(t, []string{"new"}, result.NewIDs)
	require.Equal(t, 0, result.UpdatedN)
	require.Equal(t, 1, result.UnchangedN)
	require.Equal(t, "https://api.example/v1/models", cat.Providers["openai"].EndpointUsed)
	require.True(t, cat.Providers["openai"].LastRefresh.Equal(now))

	// Unchanged upstream keeps the Unchanged counter and never reports NewIDs.
	result = mergeRefresh(cat, fetch, now.Add(time.Minute))
	require.Empty(t, result.NewIDs)
	require.Equal(t, 0, result.UpdatedN)
	require.Equal(t, len(fetch.upstream), result.UnchangedN)

	// A catalog model absent upstream is reported stale but stays in the catalog.
	cat.Providers["openai"].Models["gone"] = &ModelEntry{FirstSeen: now, LastSeen: now}
	result = mergeRefresh(cat, fetch, now.Add(2*time.Minute))
	require.Contains(t, result.StaleIDs, "gone")
	require.NotNil(t, cat.Providers["openai"].Models["gone"])

	// A changed upstream field flips the entry to Updated.
	fetch.upstream[1].OwnedBy = "openai-inc"
	result = mergeRefresh(cat, fetch, now.Add(3*time.Minute))
	require.Equal(t, 1, result.UpdatedN)
	require.Equal(t, "openai-inc", cat.Providers["openai"].Models["same"].OwnedBy)
}

func TestUpdateModelMetadataFieldRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dst  ModelMetadata
		src  ModelMetadata
		want ModelMetadata
	}{
		{
			name: "empty upstream leaves metadata untouched",
			dst:  ModelMetadata{Name: "kept", ContextLength: 128_000},
			src:  ModelMetadata{},
			want: ModelMetadata{Name: "kept", ContextLength: 128_000},
		},
		{
			name: "non-empty scalar overwrites",
			dst:  ModelMetadata{Name: "old"},
			src:  ModelMetadata{Name: "new"},
			want: ModelMetadata{Name: "new"},
		},
		{
			name: "zero scalar does not clear an existing value",
			dst:  ModelMetadata{ContextLength: 128_000},
			src:  ModelMetadata{ContextLength: 0},
			want: ModelMetadata{ContextLength: 128_000},
		},
		{
			name: "lists and maps are replaced only when provided",
			dst:  ModelMetadata{SupportedParameters: []string{"temperature"}, DefaultParameters: map[string]any{"temperature": 0.5}},
			src:  ModelMetadata{Reasoning: map[string]any{"enabled": true}},
			want: ModelMetadata{
				SupportedParameters: []string{"temperature"},
				DefaultParameters:   map[string]any{"temperature": 0.5},
				Reasoning:           map[string]any{"enabled": true},
			},
		},
		{
			name: "architecture without modality data is ignored",
			dst:  ModelMetadata{Architecture: ModelArchitecture{Modality: "text->text"}},
			src:  ModelMetadata{},
			want: ModelMetadata{Architecture: ModelArchitecture{Modality: "text->text"}},
		},
		{
			name: "pricing overwrite keeps explicit upstream prices",
			dst:  ModelMetadata{Pricing: ModelPricing{Prompt: "0.1", Completion: "0.2"}},
			src:  ModelMetadata{Pricing: ModelPricing{Prompt: "0.3", Completion: "0.4"}},
			want: ModelMetadata{Pricing: ModelPricing{Prompt: "0.3", Completion: "0.4"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.dst
			changed := updateModelMetadata(&got, tt.src)
			require.Equal(t, tt.want, got)
			require.Equal(t, !reflect.DeepEqual(tt.dst, tt.want), changed)
		})
	}

	architecture := ModelMetadata{Architecture: ModelArchitecture{Modality: "text->text"}}
	changed := updateModelMetadata(&architecture, ModelMetadata{Architecture: ModelArchitecture{Modality: "image->text"}})
	require.True(t, changed)
	require.Equal(t, "image->text", architecture.Architecture.Modality)
}
