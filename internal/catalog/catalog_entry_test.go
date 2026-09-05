package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
	"github.com/stretchr/testify/require"
)

func TestCatalogEntryWritersInitializePartialState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	states := []struct {
		name string
		cat  func() *Catalog
	}{
		{"missing provider", func() *Catalog { return &Catalog{Providers: map[string]*ProviderCatalog{}} }},
		{"nil provider", func() *Catalog { return &Catalog{Providers: map[string]*ProviderCatalog{"provider": nil}} }},
		{"nil models", func() *Catalog { return &Catalog{Providers: map[string]*ProviderCatalog{"provider": {}}} }},
		{"nil model entry", func() *Catalog {
			return &Catalog{Providers: map[string]*ProviderCatalog{"provider": {Models: map[string]*ModelEntry{"model": nil}}}}
		}},
	}
	writers := []struct {
		name  string
		write func(*Catalog) error
		check func(*testing.T, *ModelEntry)
	}{
		{
			name: "ping",
			write: func(cat *Catalog) error {
				RecordPingWithMetrics(cat, "provider", "model", true, time.Second, 0, 0, 0, 1, 2, "", now)
				return nil
			},
			check: func(t *testing.T, entry *ModelEntry) {
				require.Equal(t, now, entry.LastPing.CheckedAt)
			},
		},
		{
			name: "quality",
			write: func(cat *Catalog) error {
				return RecordQualityEvidence(cat, QualityImportRecord{Provider: "provider", Model: "model", Task: "chat", Score: 0.8}, now)
			},
			check: func(t *testing.T, entry *ModelEntry) {
				require.Equal(t, now, entry.Quality["chat"].UpdatedAt)
			},
		},
	}

	for _, state := range states {
		for _, writer := range writers {
			t.Run(state.name+"/"+writer.name, func(t *testing.T) {
				t.Parallel()
				cat := state.cat()
				require.NoError(t, writer.write(cat))

				entry := cat.Providers["provider"].Models["model"]
				require.NotNil(t, entry)
				require.Equal(t, now, entry.FirstSeen)
				require.Equal(t, now, entry.LastSeen)
				writer.check(t, entry)
			})
		}
	}

	t.Run("quality initializes root providers after validation", func(t *testing.T) {
		t.Parallel()
		cat := &Catalog{}
		require.Error(t, RecordQualityEvidence(cat, QualityImportRecord{Provider: "provider", Model: "model", Task: "chat", Score: 1.1}, now))
		require.Nil(t, cat.Providers)
		require.NoError(t, RecordQualityEvidence(cat, QualityImportRecord{Provider: "provider", Model: "model", Task: "chat", Score: 0.8}, now))
		require.NotNil(t, cat.Providers["provider"].Models["model"])
	})
}

func TestCatalogEntryWritersPreserveExistingEntry(t *testing.T) {
	t.Parallel()

	firstSeen := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	lastSeen := firstSeen.Add(time.Hour)
	now := lastSeen.Add(time.Hour)
	writers := []struct {
		name  string
		write func(*Catalog) error
	}{
		{
			name: "ping",
			write: func(cat *Catalog) error {
				RecordPingWithMetrics(cat, "provider", "model", false, time.Second, 0, 0, 0, 0, 0, "timeout", now)
				return nil
			},
		},
		{
			name: "quality",
			write: func(cat *Catalog) error {
				return RecordQualityEvidence(cat, QualityImportRecord{Provider: "provider", Model: "model", Task: "chat", Score: 0.8}, now)
			},
		},
	}

	for _, writer := range writers {
		t.Run(writer.name, func(t *testing.T) {
			t.Parallel()
			entry := &ModelEntry{FirstSeen: firstSeen, LastSeen: lastSeen, Pinned: true}
			cat := &Catalog{Providers: map[string]*ProviderCatalog{
				"provider": {Models: map[string]*ModelEntry{"model": entry}},
			}}

			require.NoError(t, writer.write(cat))
			require.Same(t, entry, cat.Providers["provider"].Models["model"])
			require.Equal(t, firstSeen, entry.FirstSeen)
			require.Equal(t, lastSeen, entry.LastSeen)
			require.True(t, entry.Pinned)
		})
	}
}

func TestRefreshInitializesEmptyAndDuplicateCatalogEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		payload  map[string]any
		check    func(*testing.T, RefreshResult, *ProviderCatalog)
	}{
		{
			name:     "empty upstream",
			provider: "openai",
			payload:  map[string]any{"data": []any{}},
			check: func(t *testing.T, result RefreshResult, pc *ProviderCatalog) {
				require.Zero(t, result.Total)
				require.Empty(t, pc.Models)
				require.False(t, pc.LastRefresh.IsZero())
			},
		},
		{
			name:     "duplicate ids",
			provider: "openai",
			payload: map[string]any{"data": []map[string]any{
				{"id": "duplicate"}, {"id": "duplicate"},
			}},
			check: func(t *testing.T, result RefreshResult, pc *ProviderCatalog) {
				require.Equal(t, 2, result.Total)
				require.Equal(t, []string{"duplicate"}, result.NewIDs)
				require.Equal(t, 1, result.UnchangedN)
				require.Len(t, pc.Models, 1)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			srv := catalogEntryModelServer(t, test.payload)

			results, cat := refreshCatalogEntry(t, test.provider, "openai", srv.URL)
			require.Len(t, results, 1)
			require.NoError(t, results[0].Err)
			test.check(t, results[0], cat.Providers[test.provider])
		})
	}
}

func TestRefreshStoresDescriptionOnlyMetadataForNewEntry(t *testing.T) {
	t.Parallel()

	srv := catalogEntryModelServer(t, map[string]any{"models": []map[string]any{
		{"name": "models/description-only", "description": "A descriptive model"},
	}})

	results, cat := refreshCatalogEntry(t, "gemini", "gemini", srv.URL)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	require.Equal(t, "A descriptive model", cat.Providers["gemini"].Models["description-only"].Metadata.Description)
}

func catalogEntryModelServer(t *testing.T, payload any) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func refreshCatalogEntry(t *testing.T, provider, providerType, baseURL string) ([]RefreshResult, *Catalog) {
	t.Helper()
	catalogPath := filepath.Join(t.TempDir(), "catalog.json")
	results, err := Refresh(context.Background(), nil, map[string]providers.Config{
		provider: {BaseURL: baseURL, ProviderType: providerType, RequiresKey: false, Enabled: true},
	}, catalogPath)
	require.NoError(t, err)
	cat, err := Load(catalogPath)
	require.NoError(t, err)
	return results, cat
}
