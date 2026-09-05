package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/llambo/internal/catalog"
)

func TestRunModelsCatalogImportQuality(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	inputPath := filepath.Join(t.TempDir(), "quality.json")
	data := []byte(`[{"provider":"openrouter","model":"qwen/qwen3-30b-a3b-instruct-2507","task":"extraction","score":1,"source":"distill"}]`)
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runModelsCatalogImportQuality(&commandIO{ctx: context.Background()}, []string{inputPath}); err != nil {
		t.Fatalf("runModelsCatalogImportQuality: %v", err)
	}

	cat, err := catalog.Load(catalogPathForHome(home))
	if err != nil {
		t.Fatal(err)
	}
	entry := cat.Providers["openrouter"].Models["qwen/qwen3-30b-a3b-instruct-2507"]
	if entry == nil {
		t.Fatal("expected imported model entry")
	}
	if !catalog.HasTag(entry, "extraction") || entry.Quality["extraction"].Score != 1 {
		t.Fatalf("expected extraction quality evidence, got %#v", entry)
	}
}
