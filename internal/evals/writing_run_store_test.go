package evals

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testWritingManifest() WritingRunManifest {
	return WritingRunManifest{
		SchemaVersion: WritingRunSchemaVersion,
		RunID:         "run-test",
		CreatedAt:     time.Unix(1, 0).UTC(),
		InputPath:     "/tmp/prompts.jsonl",
		Identity: WritingRunIdentity{
			InputSHA256: "abc", BenchmarkID: "writingbench", AdapterVersion: "v1", JudgePromptVersion: WritingJudgePromptVersion, GenerationPromptVersion: WritingGenerationPromptVersion,
			PromptIDs: []string{"p1"}, Models: []WritingModelSpec{{Provider: "p", Model: "m", MaxOutputTokens: 64}}, Judge: WritingModelSpec{Provider: "j", Model: "m", MaxOutputTokens: 32},
			Iterations: 1, Concurrency: 2, TimeoutSeconds: 300, MaxRunCostUSD: 1,
		},
	}
}

func TestWritingRunStoreRejectsOrphanedLedger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "generations.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWritingRunStore(dir, testWritingManifest()); err == nil {
		t.Fatal("expected orphaned ledger rejection")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("rejected output directory mode changed to %o", got)
	}
}

func TestWritingRunStoreResumeAndManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := testWritingManifest()
	store, err := OpenWritingRunStore(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	record := WritingGenerationRecord{Key: "g1", Attempt: 1, Status: "success", Content: "hello", ContentSHA256: writingHash("hello")}
	if err := store.AppendGeneration(record); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := OpenWritingRunStore(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	if got, ok := resumed.Generation("g1"); !ok || got.Content != "hello" {
		t.Fatalf("resumed generation = %#v, %v", got, ok)
	}
	if got := resumed.NextGenerationAttempt("g1"); got != 2 {
		t.Fatalf("next attempt = %d, want 2", got)
	}
	mismatch := manifest
	mismatch.Identity.Judge.Model = "different"
	if _, err := OpenWritingRunStore(dir, mismatch); err == nil {
		t.Fatal("expected manifest mismatch")
	}
}

func TestWritingRunStoreConcurrentAppend(t *testing.T) {
	t.Parallel()
	store, err := OpenWritingRunStore(t.TempDir(), testWritingManifest())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := store.AppendGeneration(WritingGenerationRecord{Key: fmt.Sprintf("g-%d", i), Attempt: 1, Status: "success"}); err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	generations, _ := store.Records()
	if len(generations) != 24 {
		t.Fatalf("generation records = %d, want 24", len(generations))
	}
}

func TestWritingRunStoreUsesPrivateModes(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "run")
	store, err := OpenWritingRunStore(dir, testWritingManifest())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, path := range []string{dir, filepath.Join(dir, "manifest.json"), filepath.Join(dir, "generations.jsonl"), filepath.Join(dir, "judgments.jsonl")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %o, want %o", path, got, want)
		}
	}
}
