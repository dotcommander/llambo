package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/stretchr/testify/require"
)

func TestCatalogCommandUpdatesPreserveEachOther(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("subprocess integration")
	}
	home := t.TempDir()
	path := filepath.Join(home, ".config", "llambo", "catalog.json")
	prior := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, catalog.Save(path, &catalog.Catalog{Version: 1, Providers: map[string]*catalog.ProviderCatalog{
		"p": {Models: map[string]*catalog.ModelEntry{
			"m": {Pinned: true}, "mtagged": {Tags: []string{"temp"}}, "prompt": {QuarantineUntil: prior},
		}},
	}}))
	quality, err := json.Marshal([]catalog.QualityImportRecord{
		{Provider: "p", Model: "m", Task: "chat", Score: 0.8},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, "quality.json"), quality, 0o600))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	type child struct {
		cmd *exec.Cmd
		out *bytes.Buffer
	}
	children := make([]child, 0, 4)
	for _, mode := range []string{"tag", "untag", "quality", "ping", "prompt"} {
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCatalogCommandUpdateProcess$")
		command.Env = catalogCommandTestEnv(home, mode)
		output := &bytes.Buffer{}
		command.Stdout, command.Stderr = output, output
		require.NoError(t, command.Start())
		t.Cleanup(func() {
			if command.ProcessState == nil {
				cancel()
				_ = command.Wait()
			}
		})
		children = append(children, child{command, output})
	}
	for _, child := range children {
		require.NoError(t, child.cmd.Wait(), child.out.String())
	}
	cat, err := catalog.Load(path)
	require.NoError(t, err)
	models := cat.Providers["p"].Models
	require.True(t, models["m"].Pinned)
	require.True(t, catalog.HasTag(models["m"], "retained"))
	require.NotNil(t, models["mtagged"])
	require.False(t, catalog.HasTag(models["mtagged"], "temp"))
	require.Equal(t, 0.8, models["m"].Quality["chat"].Score)
	require.False(t, models["ping"].QuarantineUntil.IsZero())
	require.Equal(t, 7, models["ping"].LastPing.TokensOut)
	require.Equal(t, prior, models["prompt"].QuarantineUntil)
	require.True(t, models["prompt"].LastPing.Success)
}

// Each helper gets a private process environment; parallel parent tests never
// alter HOME or the command package's global flag state.
func TestCatalogCommandUpdateProcess(t *testing.T) {
	t.Parallel()
	mode := os.Getenv("LLAMBO_CATALOG_UPDATE_TEST")
	if mode == "" {
		t.Skip("subprocess helper")
	}
	ctx := context.Background()
	var err error
	switch mode {
	case "tag":
		err = Execute(ctx, []string{"models", "catalog", "tag", "p", "m", "retained"}, os.Stdout, os.Stderr)
	case "untag":
		err = Execute(ctx, []string{"models", "catalog", "untag", "p", "mtagged", "temp"}, os.Stdout, os.Stderr)
	case "quality":
		err = Execute(ctx, []string{"models", "catalog", "import-quality", filepath.Join(os.Getenv("HOME"), "quality.json")}, os.Stdout, os.Stderr)
	case "ping":
		err = recordPingCatalogHealth(ctx, []PingResult{{Provider: "p", Model: "ping", Success: true, Latency: catalog.SlowPingThreshold + time.Second, TokensOut: 7}})
	case "prompt":
		err = recordPromptCatalogHealth(ctx, []PromptResult{{Provider: "p", Model: "prompt", Latency: catalog.SlowPingThreshold + time.Second}})
	case "discover":
		err = Execute(ctx, []string{"--config", filepath.Join(os.Getenv("HOME"), ".config", "llambo", "config.json"),
			"models", "discover-free", "-P", "p", "--pin", "--timeout-seconds", "5"}, os.Stdout, os.Stderr)
	default:
		t.Fatalf("unknown subprocess mode %q", mode)
	}
	require.NoError(t, err)
}

func catalogCommandTestEnv(home, mode string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HOME=") && !strings.HasPrefix(entry, "LLAMBO_CATALOG_UPDATE_TEST=") {
			env = append(env, entry)
		}
	}
	return append(env, "HOME="+home, "LLAMBO_CATALOG_UPDATE_TEST="+mode)
}

func TestCatalogHealthRecordersSkipEmptyResults(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, recordPingCatalogHealth(ctx, nil))
	require.NoError(t, recordPromptCatalogHealth(ctx, nil))
}
