package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/stretchr/testify/require"
)

func TestDiscoverFreePreservesChangesDuringProbe(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("subprocess integration")
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "llambo")
	path := filepath.Join(dir, "catalog.json")
	require.NoError(t, catalog.Save(path, &catalog.Catalog{Version: 1, Providers: map[string]*catalog.ProviderCatalog{}}))
	models, err := json.Marshal(map[string]any{"data": []map[string]string{{"id": "m"}}})
	require.NoError(t, err)
	chunk, err := json.Marshal(map[string]any{
		"id": "probe", "object": "chat.completion.chunk", "model": "m",
		"choices": []map[string]any{{"index": 0, "delta": map[string]string{"content": "OK"}, "finish_reason": "stop"}},
	})
	require.NoError(t, err)
	updates := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(models)
		case "/v1/chat/completions":
			err := catalog.Update(r.Context(), path, func(cat *catalog.Catalog) error {
				catalog.AddTag(cat.Providers["p"].Models["m"], "during-probe")
				return nil
			})
			select {
			case updates <- err:
			default:
			}
			if err != nil {
				http.Error(w, "catalog update failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	config, err := json.Marshal(map[string]any{"default_provider": "p", "providers": map[string]any{
		"p": map[string]any{"base_url": server.URL, "provider_type": "openai", "model": "m", "api_keys": []string{"local-test-key"}, "requires_key": false, "enabled": true},
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), config, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "model-costs.csv"), []byte("provider,model,input_per_1m_usd,output_per_1m_usd\np,m,0,0\n"), 0o600))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCatalogCommandUpdateProcess$")
	command.Env = catalogCommandTestEnv(home, "discover")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	cat, err := catalog.Load(path)
	require.NoError(t, err)
	entry := cat.Providers["p"].Models["m"]
	require.True(t, entry.LastPing.Success, entry.LastPing.Error)
	select {
	case err := <-updates:
		require.NoError(t, err)
	default:
		t.Fatalf("probe did not perform the concurrent catalog update; command output:\n%s", output)
	}
	require.True(t, catalog.HasTag(entry, "during-probe"))
	require.True(t, entry.Pinned)
}
