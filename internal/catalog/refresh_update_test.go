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

func TestRefreshFetchesOutsideUpdateLockAndPreservesInterveningState(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("HTTP integration")
	}

	path := filepath.Join(t.TempDir(), "catalog.json")
	seenAt := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, Save(path, &Catalog{Version: 1, Providers: map[string]*ProviderCatalog{
		"openai": {Models: map[string]*ModelEntry{"model": {FirstSeen: seenAt, LastSeen: seenAt}}},
	}}))
	payload, err := json.Marshal(map[string]any{"data": []map[string]any{{"id": "model", "name": "Refreshed"}}})
	require.NoError(t, err)
	updated := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := Update(r.Context(), path, func(cat *Catalog) error {
			entry := cat.Providers["openai"].Models["model"]
			entry.Pinned = true
			entry.Tags = []string{"smart"}
			entry.LastPing = PingState{Success: true, CheckedAt: seenAt.Add(time.Minute)}
			return nil
		})
		select {
		case updated <- err:
		default:
		}
		if err != nil {
			http.Error(w, "catalog update failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	results, err := Refresh(ctx, nil, map[string]providers.Config{
		"openai": {BaseURL: srv.URL, ProviderType: "openai", RequiresKey: false, Enabled: true},
	}, path)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	select {
	case err := <-updated:
		require.NoError(t, err)
	default:
		t.Fatal("refresh did not fetch models")
	}

	cat, err := Load(path)
	require.NoError(t, err)
	entry := cat.Providers["openai"].Models["model"]
	require.True(t, entry.Pinned)
	require.Equal(t, []string{"smart"}, entry.Tags)
	require.True(t, entry.LastPing.Success)
	require.Equal(t, "Refreshed", entry.Metadata.Name)
}
