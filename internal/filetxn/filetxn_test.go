package filetxn

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestTransactionsAcrossProcesses(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("subprocess transaction fixture")
	}
	path := filepath.Join(t.TempDir(), "counter")
	if err := WriteAtomic(path, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Go(func() {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTransactionWorker$")
			cmd.Env = append(os.Environ(), "LLAMBO_TXN_FIXTURE="+path)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Logf("worker: %s", output)
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "30" {
		t.Fatalf("lost update: %s", data)
	}
}

func TestTransactionWorker(t *testing.T) {
	t.Parallel()
	path := os.Getenv("LLAMBO_TXN_FIXTURE")
	if path == "" {
		t.Skip("subprocess only")
	}
	for range 10 {
		if err := WithLock(t.Context(), path, func(path string) error {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			count, err := strconv.Atoi(string(data))
			if err != nil {
				return err
			}
			return WriteAtomic(path, []byte(strconv.Itoa(count+1)), 0o600)
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAliasesAndFailureCleanup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	alias := filepath.Join(dir, "alias")
	if err := WriteAtomic(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if err := WithLock(t.Context(), alias, func(path string) error { return WriteAtomic(path, []byte("new"), 0o600) }); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(alias); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("alias replaced", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Fatal("target unchanged", err)
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(dangling, []byte("bad"), 0o600); err == nil {
		t.Fatal("dangling alias accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	if err := WithLock(ctx, target, func(string) error { called = true; return nil }); err == nil || called {
		t.Fatal("canceled mutation")
	}
	destination := filepath.Join(dir, "directory")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(destination, []byte("bad"), 0o600); err == nil {
		t.Fatal("directory replacement accepted")
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".*.tmp-*"))
	if err != nil || len(temps) != 0 {
		t.Fatal("temporary leak", temps, err)
	}
}
