package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestCatalogStatusLabelCombinesPinAvoidAndQuarantine(t *testing.T) {
	t.Parallel()
	if got := catalogStatusLabel(&catalog.ModelEntry{Pinned: true}); got != "pinned" {
		t.Fatalf("pinned status = %q, want pinned", got)
	}
	if got := catalogStatusLabel(&catalog.ModelEntry{Avoid: true, AvoidReason: "weak"}); got != "avoid: weak" {
		t.Fatalf("avoid status = %q, want avoid: weak", got)
	}
	if got := catalogStatusLabel(&catalog.ModelEntry{}); got != "" {
		t.Fatalf("plain status = %q, want empty", got)
	}

	// Avoid state replaces the pinned label; quarantine appends to either.
	quarantined := &catalog.ModelEntry{Pinned: true, Avoid: true, QuarantineUntil: time.Now().Add(time.Hour)}
	if got, want := catalogStatusLabel(quarantined), "avoid, quarantine until "; !strings.HasPrefix(got, want) {
		t.Fatalf("status = %q, want prefix %q", got, want)
	}
	pinnedQuarantined := &catalog.ModelEntry{Pinned: true, QuarantineUntil: time.Now().Add(time.Hour)}
	if got, want := catalogStatusLabel(pinnedQuarantined), "pinned, quarantine until "; !strings.HasPrefix(got, want) {
		t.Fatalf("status = %q, want prefix %q", got, want)
	}
}

func TestCatalogRowMatchesFiltersRequireTagAndFreeCost(t *testing.T) {
	t.Parallel()
	tagged := &invocationOptions{catalogFilterTag: "vision"}
	if !tagged.catalogRowMatchesFilters(&catalog.ModelEntry{Tags: []string{"vision"}}, &catalog.ProviderCatalog{}, catalog.CostPaid) {
		t.Fatal("matching tag must pass")
	}
	if tagged.catalogRowMatchesFilters(&catalog.ModelEntry{Tags: []string{"other"}}, &catalog.ProviderCatalog{}, catalog.CostPaid) {
		t.Fatal("non-matching tag must be filtered")
	}

	freeOnly := &invocationOptions{catalogFilterFree: true}
	if freeOnly.catalogRowMatchesFilters(&catalog.ModelEntry{}, &catalog.ProviderCatalog{}, catalog.CostPaid) {
		t.Fatal("paid entry must fail the free filter")
	}
	if !freeOnly.catalogRowMatchesFilters(&catalog.ModelEntry{}, &catalog.ProviderCatalog{}, catalog.CostFree) {
		t.Fatal("free entry must pass the free filter")
	}
}
