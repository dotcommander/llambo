package evals

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritingInitializationFailureClosesAcquiredLedgers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var opened []*os.File
	failedOpen := func(path string, flags int, mode os.FileMode) (*os.File, error) {
		if filepath.Base(path) == "judgments.jsonl" {
			return nil, os.ErrPermission
		}
		file, err := os.OpenFile(path, flags, mode)
		if file != nil {
			opened = append(opened, file)
		}
		return file, err
	}
	if store, err := acquireWritingRunStore(dir, testWritingManifest(), failedOpen); err == nil {
		_ = store.Close()
		t.Fatal("expected initialization error")
	}
	if len(opened) != 2 {
		t.Fatalf("opened %d descriptors", len(opened))
	}
	for _, file := range opened {
		if _, err := file.Stat(); err == nil {
			t.Fatalf("descriptor remains open: %s", file.Name())
		}
	}
	store, err := OpenWritingRunStore(dir, testWritingManifest())
	if err != nil {
		t.Fatal("failed initializer retained lock", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CheckNoInDoubtDispatch(); err != nil {
		t.Fatal(err)
	}
}

func assertWritingLedgersClosed(t *testing.T, opened []*os.File) {
	t.Helper()
	if len(opened) != 3 {
		t.Fatalf("opened %d ledgers", len(opened))
	}
	for _, file := range opened {
		if _, err := file.Stat(); err == nil {
			t.Fatalf("descriptor leaked: %s", file.Name())
		}
	}
}

func writingChmodFailureOpener(name string, opened *[]*os.File) func(string, int, os.FileMode) (*os.File, error) {
	return func(path string, flags int, mode os.FileMode) (*os.File, error) {
		file, err := os.OpenFile(path, flags, mode)
		if err != nil {
			return nil, err
		}
		*opened = append(*opened, file)
		// Closing this acquired descriptor makes its subsequent Chmod fail deterministically.
		if filepath.Base(path) == name {
			if closeErr := file.Close(); closeErr != nil {
				return nil, closeErr
			}
		}
		return file, nil
	}
}

func TestWritingInitializationChmodFailureClosesEveryLedger(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"intents.jsonl", "generations.jsonl", "judgments.jsonl"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var opened []*os.File
			openLedger := writingChmodFailureOpener(name, &opened)
			if store, err := acquireWritingRunStore(dir, testWritingManifest(), openLedger); err == nil {
				_ = store.Close()
				t.Fatal("chmod failure accepted")
			}
			assertWritingLedgersClosed(t, opened)
			resumed, err := OpenWritingRunStore(dir, testWritingManifest())
			if err != nil {
				t.Fatal("initializer leaked lock", err)
			}
			t.Cleanup(func() { _ = resumed.Close() })
		})
	}
}
