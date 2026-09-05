package catalog

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCatalogUpdateProcessHelper(t *testing.T) {
	t.Parallel()
	if os.Getenv("LLAMBO_CATALOG_UPDATE_HELPER") != "1" {
		return
	}
	path := os.Getenv("LLAMBO_CATALOG_PATH")
	id := os.Getenv("LLAMBO_CATALOG_MODEL")
	hold := os.Getenv("LLAMBO_CATALOG_HOLD") == "1"
	fmt.Fprintln(os.Stdout, "attempting")
	err := Update(context.Background(), path, func(cat *Catalog) error {
		fmt.Fprintln(os.Stdout, "locked")
		if hold {
			_, _ = os.Stdin.Read(make([]byte, 1))
		}
		pc := ensureProviderCatalog(cat, "provider")
		ensureModelEntry(pc, id, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func TestUpdateCoordinatesConcurrentProcessesAndReusesLock(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("subprocess integration")
	}

	path := filepath.Join(t.TempDir(), "catalog.json")
	first, firstIn, firstOut := startCatalogUpdateProcess(t, path, "first", true)
	require.Equal(t, "attempting", <-firstOut)
	require.Equal(t, "locked", <-firstOut)

	second, _, secondOut := startCatalogUpdateProcess(t, path, "second", false)
	require.Equal(t, "attempting", <-secondOut)
	require.NoError(t, firstIn.Close())
	require.NoError(t, first.Wait())
	require.Equal(t, "locked", <-secondOut)
	require.NoError(t, second.Wait())

	require.NoError(t, Update(context.Background(), path, func(cat *Catalog) error {
		pc := ensureProviderCatalog(cat, "provider")
		ensureModelEntry(pc, "third", time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
		return nil
	}))
	cat, err := Load(path)
	require.NoError(t, err)
	for _, id := range []string{"first", "second", "third"} {
		require.Contains(t, cat.Providers["provider"].Models, id)
	}
	_, err = os.Stat(path + ".lock")
	require.NoError(t, err)
}

func startCatalogUpdateProcess(t *testing.T, path, id string, hold bool) (*exec.Cmd, io.WriteCloser, <-chan string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCatalogUpdateProcessHelper$")
	cmd.Env = append(os.Environ(),
		"LLAMBO_CATALOG_UPDATE_HELPER=1",
		"LLAMBO_CATALOG_PATH="+path,
		"LLAMBO_CATALOG_MODEL="+id,
	)
	if hold {
		cmd.Env = append(cmd.Env, "LLAMBO_CATALOG_HOLD=1")
	} else {
		cmd.Env = append(cmd.Env, "LLAMBO_CATALOG_HOLD=0")
	}
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			cancel()
			_ = cmd.Wait()
		}
	})
	lines := make(chan string, 2)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if line := scanner.Text(); line == "attempting" || line == "locked" {
				lines <- line
			}
		}
		close(lines)
	}()
	return cmd, stdin, lines
}
