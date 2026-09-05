package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
	"github.com/stretchr/testify/require"
)

func TestResolveTextChatSelectionForwardsOptions(t *testing.T) {
	t.Parallel()

	path := writeSelectionCatalog(t, &catalog.Catalog{Version: 1, Providers: map[string]*catalog.ProviderCatalog{
		"alpha": {Models: map[string]*catalog.ModelEntry{
			"tagged": {Tags: []string{"smart"}, QuarantineUntil: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)},
		}},
		"beta": {Models: map[string]*catalog.ModelEntry{
			"other": {Tags: []string{"smart"}},
		}},
	}})
	selected, err := resolveTextChatSelection(&bytes.Buffer{}, path, map[string]providers.Config{
		"alpha": {Enabled: true, Priority: 2, Model: "tagged"},
		"beta":  {Enabled: true, Priority: 1, Model: "other"},
	}, map[string]costs.ModelCost{
		costs.Key("alpha", "tagged"): {InputExplicit: true, OutputExplicit: true},
		costs.Key("beta", "other"):   {InputPer1M: 1, OutputPer1M: 1, InputExplicit: true, OutputExplicit: true},
	}, catalog.SelectorOptions{
		Selector:          "tag:smart",
		ProviderFilter:    "alpha",
		FreeOnly:          true,
		IncludeQuarantine: true,
		Now:               time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"alpha/tagged"}, selectionIDs(selected))
	require.Equal(t, catalog.CostFree, selected[0].CostStatus)
}

func TestResolveTextChatSelectionPreservesCatalogOrdering(t *testing.T) {
	t.Parallel()

	path := writeSelectionCatalog(t, &catalog.Catalog{Version: 1, Providers: map[string]*catalog.ProviderCatalog{
		"alpha": {Models: map[string]*catalog.ModelEntry{"z": {}, "a": {}}},
		"beta":  {Models: map[string]*catalog.ModelEntry{"b": {}}},
	}})
	selected, err := resolveTextChatSelection(&bytes.Buffer{}, path, map[string]providers.Config{
		"alpha": {Enabled: true, Priority: 2},
		"beta":  {Enabled: true, Priority: 1},
	}, nil, catalog.SelectorOptions{Selector: "all"})
	require.NoError(t, err)
	require.Equal(t, []string{"beta/b", "alpha/a", "alpha/z"}, selectionIDs(selected))
}

func TestResolveTextChatSelectionReportsOnlyNonChatMatches(t *testing.T) {
	t.Parallel()

	path := writeSelectionCatalog(t, &catalog.Catalog{Version: 1, Providers: map[string]*catalog.ProviderCatalog{
		"openai": {Models: map[string]*catalog.ModelEntry{
			"image-only": {Metadata: catalog.ModelMetadata{Architecture: catalog.ModelArchitecture{OutputModalities: []string{"image"}}}},
		}},
	}})
	var errOut bytes.Buffer
	selected, err := resolveTextChatSelection(&errOut, path, map[string]providers.Config{
		"openai": {Enabled: true, Model: "image-only"},
	}, nil, catalog.SelectorOptions{Selector: "all"})
	require.Nil(t, selected)
	require.EqualError(t, err, `no text chat-capable models match selector "all"`)
	require.Equal(t, "Chat filter: skipped openai/image-only (model does not produce text output)\nChat filter: skipped 1 non-text chat target(s)\n", errOut.String())
}

func TestResolveTextChatSelectionWrapsMalformedCatalog(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))
	_, err := resolveTextChatSelection(&bytes.Buffer{}, path, map[string]providers.Config{
		"openai": {Enabled: true, Model: "model"},
	}, nil, catalog.SelectorOptions{Selector: "all"})
	require.ErrorContains(t, err, "load catalog: parse catalog")
}

func writeSelectionCatalog(t *testing.T, cat *catalog.Catalog) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, catalog.Save(path, cat))
	return path
}

func selectionIDs(targets []catalog.ModelTarget) []string {
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.Provider+"/"+target.Model)
	}
	return ids
}
