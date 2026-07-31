package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadRoundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	now := time.Now().UTC().Truncate(time.Second)
	cat := &Catalog{
		Version: 1,
		Providers: map[string]*ProviderCatalog{
			"openai": {
				LastRefresh:  now,
				EndpointUsed: "https://api.openai.com/v1/models",
				Models: map[string]*ModelEntry{
					"gpt-4o": {
						FirstSeen: now,
						LastSeen:  now,
						OwnedBy:   "openai",
					},
					"gpt-5": {
						FirstSeen:       now,
						LastSeen:        now,
						UpstreamCreated: now,
						OwnedBy:         "openai",
						Pinned:          true,
					},
				},
			},
			"groq": {
				LastRefresh:  now,
				EndpointUsed: "https://api.groq.com/openai/v1/models",
				Models: map[string]*ModelEntry{
					"llama3-8b": {
						FirstSeen: now,
						LastSeen:  now,
					},
				},
			},
		},
	}

	require.NoError(t, Save(path, cat))

	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, cat.Version, loaded.Version)
	require.Len(t, loaded.Providers, 2)

	openai := loaded.Providers["openai"]
	require.NotNil(t, openai)
	require.True(t, openai.LastRefresh.Equal(now))
	require.Equal(t, "https://api.openai.com/v1/models", openai.EndpointUsed)
	require.Len(t, openai.Models, 2)

	gpt5 := openai.Models["gpt-5"]
	require.NotNil(t, gpt5)
	require.True(t, gpt5.Pinned)
	require.True(t, gpt5.UpstreamCreated.Equal(now))
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	cat, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cat)
	require.Equal(t, 1, cat.Version)
	require.NotNil(t, cat.Providers)
	require.Empty(t, cat.Providers)
}

func TestSaveAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	cat := &Catalog{
		Version:   1,
		Providers: make(map[string]*ProviderCatalog),
	}
	require.NoError(t, Save(path, cat))

	// Final file must exist.
	_, err := os.Stat(path)
	require.NoError(t, err)

	// Temp file must not exist after Rename.
	_, err = os.Stat(path + ".tmp")
	require.True(t, os.IsNotExist(err), "temp file should not exist after atomic save")
}

func TestPinnedModels(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	cat := &Catalog{
		Version: 1,
		Providers: map[string]*ProviderCatalog{
			"openai": {
				Models: map[string]*ModelEntry{
					"gpt-5":      {Pinned: true, FirstSeen: now, LastSeen: now},
					"gpt-5-mini": {Pinned: true, FirstSeen: now, LastSeen: now},
					"gpt-4o":     {FirstSeen: now, LastSeen: now},
				},
			},
			"groq": {
				Models: map[string]*ModelEntry{
					"llama": {Pinned: true, Avoid: true, FirstSeen: now, LastSeen: now},
				},
			},
			"disabled": {
				Models: map[string]*ModelEntry{
					"skip": {Pinned: true, FirstSeen: now, LastSeen: now},
				},
			},
		},
	}

	got := PinnedModels(cat, map[string]providers.Config{
		"openai":   {Enabled: true},
		"groq":     {Enabled: true},
		"disabled": {Enabled: false},
	})

	require.Equal(t, map[string][]string{
		"openai": {"gpt-5", "gpt-5-mini"},
	}, got)
}
