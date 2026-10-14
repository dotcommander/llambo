package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
)

func TestProviderCatalogSummaryCountsPinnedAvoidedAndRefreshAge(t *testing.T) {
	t.Parallel()
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"tracked": {
			LastRefresh: time.Now().Add(-2 * time.Hour),
			Models: map[string]*catalog.ModelEntry{
				"a": {Pinned: true},
				"b": {Avoid: true},
				"c": {Pinned: true, Avoid: true},
			},
		},
		"empty": {Models: map[string]*catalog.ModelEntry{}},
	}}

	summary := providerCatalogSummary(cat, "tracked")
	if summary.models != 3 || summary.pinned != 2 || summary.avoided != 2 {
		t.Fatalf("summary = %d/%d/%d, want 3/2/2", summary.models, summary.pinned, summary.avoided)
	}
	if !strings.HasPrefix(summary.lastRefresh, "2h") || !strings.HasSuffix(summary.lastRefresh, " ago") {
		t.Fatalf("lastRefresh = %q, want a 2h age label", summary.lastRefresh)
	}

	summary = providerCatalogSummary(cat, "empty")
	if summary.models != 0 || summary.pinned != 0 || summary.avoided != 0 || summary.lastRefresh != never1 {
		t.Fatalf("empty provider summary = %d/%d/%d/%q, want zeroes and %q", summary.models, summary.pinned, summary.avoided, summary.lastRefresh, never1)
	}

	summary = providerCatalogSummary(cat, "missing")
	if summary.models != 0 || summary.pinned != 0 || summary.avoided != 0 || summary.lastRefresh != never1 {
		t.Fatalf("missing provider summary = %d/%d/%d/%q, want zeroes and %q", summary.models, summary.pinned, summary.avoided, summary.lastRefresh, never1)
	}
}

func TestTruncatedBaseURLKeepsShortAndEllipsizesLong(t *testing.T) {
	t.Parallel()
	exact := strings.Repeat("a", 40)
	if got := truncatedBaseURL(exact); got != exact {
		t.Fatalf("exact-length URL changed: %q", got)
	}
	long := exact + "X"
	if got, want := truncatedBaseURL(long), long[:37]+"..."; got != want {
		t.Fatalf("truncated = %q, want %q", got, want)
	}
}
