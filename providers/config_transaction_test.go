package providers

import (
	"encoding/json"
	"github.com/dotcommander/llambo/internal/filetxn"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigTransactionsPreserveConcurrentChangesAndRawKeys(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	initial := &GlobalConfig{DefaultProvider: "p", Providers: map[string]Config{"p": {Model: "Case/Model", EnvVar: "UNMATERIALIZED_FIXTURE_KEY"}}}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := filetxn.WriteAtomic(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() {
		errs <- updateGlobalConfigPath(t.Context(), path, func(cfg *GlobalConfig) error { cfg.MaxOutputCost = 3; return nil })
	})
	wg.Go(func() {
		errs <- updateGlobalConfigPath(t.Context(), path, func(cfg *GlobalConfig) error { cfg.Blocklist = []string{"p:blocked"}; return nil })
	})
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got GlobalConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.MaxOutputCost != 3 || len(got.Blocklist) != 1 || got.Providers["p"].APIKey != "" || got.Providers["p"].Model != "Case/Model" {
		t.Fatalf("transaction changed raw data or lost updates: %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions=%o", info.Mode().Perm())
	}
}
