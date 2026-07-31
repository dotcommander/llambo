package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
	"github.com/stretchr/testify/require"
)

// openaiModelServer returns a test server that responds with an OpenAI-style model list.
func openaiModelServer(t *testing.T, models []struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"data": models,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
}

// errorServer returns a test server that always returns a 401.
func errorServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
}

func TestApply_NewModel(t *testing.T) {
	t.Parallel()

	srv := openaiModelServer(t, []struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}{
		{ID: "gpt-5", OwnedBy: "openai", Created: 1700000000},
	})
	defer srv.Close()

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")

	cfgs := map[string]providers.Config{
		"openai": {
			BaseURL:      srv.URL,
			ProviderType: "openai",
			RequiresKey:  false,
			Enabled:      true,
			Model:        "gpt-5",
		},
	}

	before := time.Now().UTC().Add(-time.Second)
	results, err := Refresh(context.Background(), nil, cfgs, catalogPath)
	after := time.Now().UTC().Add(time.Second)

	require.NoError(t, err)
	require.Len(t, results, 1)
	r := results[0]
	require.NoError(t, r.Err)
	require.Equal(t, "openai", r.Provider)
	require.Equal(t, 1, r.Total)
	require.Len(t, r.NewIDs, 1)
	require.Equal(t, "gpt-5", r.NewIDs[0])

	cat, err := Load(catalogPath)
	require.NoError(t, err)
	entry := cat.Providers["openai"].Models["gpt-5"]
	require.NotNil(t, entry)
	require.True(t, entry.FirstSeen.After(before) && entry.FirstSeen.Before(after))
	require.True(t, entry.LastSeen.Equal(entry.FirstSeen), "first_seen == last_seen on first observation")
}

func TestApply_OpenRouterMetadata(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"data": []map[string]any{
				{
					"id":             "qwen/qwen3-30b-a3b-instruct-2507",
					"canonical_slug": "qwen/qwen3-30b-a3b-instruct-2507",
					"name":           "Qwen: Qwen3 30B A3B Instruct",
					"created":        int64(1700000000),
					"context_length": 128000,
					"architecture": map[string]any{
						"modality":          "text->text",
						"input_modalities":  []string{"text"},
						"output_modalities": []string{"text"},
						"tokenizer":         "Qwen3",
						"instruct_type":     nil,
					},
					"pricing": map[string]any{
						"prompt":     "0.000000084",
						"completion": "0.000000193",
					},
					"top_provider": map[string]any{
						"context_length":        128000,
						"max_completion_tokens": 32768,
						"is_moderated":          false,
					},
					"supported_parameters": []string{"max_tokens", "reasoning", "tools"},
					"reasoning":            map[string]any{"mandatory": false, "default_enabled": true},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	cfgs := map[string]providers.Config{
		"openrouter": {
			BaseURL:      srv.URL,
			ProviderType: "openrouter",
			RequiresKey:  false,
			Enabled:      true,
			Model:        "qwen/qwen3-30b-a3b-instruct-2507",
		},
	}

	results, err := Refresh(context.Background(), nil, cfgs, catalogPath)
	require.NoError(t, err)
	require.NoError(t, results[0].Err)

	cat, err := Load(catalogPath)
	require.NoError(t, err)
	entry := cat.Providers["openrouter"].Models["qwen/qwen3-30b-a3b-instruct-2507"]
	require.NotNil(t, entry)
	require.Equal(t, 128000, entry.Metadata.ContextLength)
	require.Equal(t, "0.000000084", entry.Metadata.Pricing.Prompt)
	require.Equal(t, "Qwen3", entry.Metadata.Architecture.Tokenizer)
	require.Equal(t, []string{"max_tokens", "reasoning", "tools"}, entry.Metadata.SupportedParameters)
	require.Equal(t, true, entry.Metadata.Reasoning["default_enabled"])
}

func TestApply_GeminiMetadata(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"models": []map[string]any{
				{
					"name":                       "models/gemini-2.5-flash",
					"displayName":                "Gemini 2.5 Flash",
					"description":                "Stable text and multimodal model",
					"inputTokenLimit":            1048576,
					"outputTokenLimit":           65536,
					"supportedGenerationMethods": []string{"generateContent", "countTokens"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	cfgs := map[string]providers.Config{
		"gemini": {
			BaseURL:      srv.URL,
			ProviderType: "gemini",
			RequiresKey:  false,
			Enabled:      true,
			Model:        "gemini-2.5-flash",
		},
	}

	results, err := Refresh(context.Background(), nil, cfgs, catalogPath)
	require.NoError(t, err)
	require.NoError(t, results[0].Err)

	cat, err := Load(catalogPath)
	require.NoError(t, err)
	entry := cat.Providers["gemini"].Models["gemini-2.5-flash"]
	require.NotNil(t, entry)
	require.Equal(t, "Gemini 2.5 Flash", entry.Metadata.Name)
	require.Equal(t, "Stable text and multimodal model", entry.Metadata.Description)
	require.Equal(t, 1048576, entry.Metadata.InputTokenLimit)
	require.Equal(t, 65536, entry.Metadata.OutputTokenLimit)
	require.Equal(t, []string{"generateContent", "countTokens"}, entry.Metadata.SupportedGenerationMethods)
}

func TestApply_ExistingModel_UpdatesLastSeen(t *testing.T) {
	t.Parallel()

	srv := openaiModelServer(t, []struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}{
		{ID: "gpt-4o", OwnedBy: "openai"},
	})
	defer srv.Close()

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")

	// Seed catalog with an older entry.
	oldTime := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	seed := &Catalog{
		Version: 1,
		Providers: map[string]*ProviderCatalog{
			"openai": {
				LastRefresh:  oldTime,
				EndpointUsed: "old",
				Models: map[string]*ModelEntry{
					"gpt-4o": {FirstSeen: oldTime, LastSeen: oldTime, OwnedBy: "openai"},
				},
			},
		},
	}
	require.NoError(t, Save(catalogPath, seed))

	cfgs := map[string]providers.Config{
		"openai": {
			BaseURL:      srv.URL,
			ProviderType: "openai",
			RequiresKey:  false,
			Enabled:      true,
			Model:        "gpt-4o",
		},
	}

	results, err := Refresh(context.Background(), nil, cfgs, catalogPath)
	require.NoError(t, err)
	require.NoError(t, results[0].Err)

	cat, err := Load(catalogPath)
	require.NoError(t, err)
	entry := cat.Providers["openai"].Models["gpt-4o"]
	require.NotNil(t, entry)
	require.True(t, entry.FirstSeen.Equal(oldTime), "first_seen must be preserved")
	require.True(t, entry.LastSeen.After(oldTime), "last_seen must advance")
}

func TestApply_FetchError_PreservesData(t *testing.T) {
	t.Parallel()

	srv := errorServer(t)
	defer srv.Close()

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")

	// Seed catalog with existing data for the failing provider.
	oldTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seed := &Catalog{
		Version: 1,
		Providers: map[string]*ProviderCatalog{
			"openai": {
				LastRefresh:  oldTime,
				EndpointUsed: "old",
				Models: map[string]*ModelEntry{
					"gpt-4o": {FirstSeen: oldTime, LastSeen: oldTime},
				},
			},
		},
	}
	require.NoError(t, Save(catalogPath, seed))

	cfgs := map[string]providers.Config{
		"openai": {
			BaseURL:      srv.URL,
			ProviderType: "openai",
			RequiresKey:  false,
			Enabled:      true,
			Model:        "gpt-4o",
		},
	}

	results, err := Refresh(context.Background(), nil, cfgs, catalogPath)
	require.NoError(t, err) // top-level error is nil
	require.Len(t, results, 1)
	require.Error(t, results[0].Err, "per-provider error must be set")

	// Catalog data for the failing provider must be unchanged.
	cat, err := Load(catalogPath)
	require.NoError(t, err)
	entry := cat.Providers["openai"].Models["gpt-4o"]
	require.NotNil(t, entry, "existing entry must survive a fetch error")
	require.True(t, entry.LastSeen.Equal(oldTime), "last_seen must not advance on error")
}

func TestApply_StaleDetection(t *testing.T) {
	t.Parallel()

	// Upstream returns only gpt-5; catalog has gpt-4o too.
	srv := openaiModelServer(t, []struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}{
		{ID: "gpt-5", OwnedBy: "openai"},
	})
	defer srv.Close()

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")

	oldTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seed := &Catalog{
		Version: 1,
		Providers: map[string]*ProviderCatalog{
			"openai": {
				LastRefresh: oldTime,
				Models: map[string]*ModelEntry{
					"gpt-5":  {FirstSeen: oldTime, LastSeen: oldTime},
					"gpt-4o": {FirstSeen: oldTime, LastSeen: oldTime},
				},
			},
		},
	}
	require.NoError(t, Save(catalogPath, seed))

	cfgs := map[string]providers.Config{
		"openai": {
			BaseURL:      srv.URL,
			ProviderType: "openai",
			RequiresKey:  false,
			Enabled:      true,
			Model:        "gpt-5",
		},
	}

	results, err := Refresh(context.Background(), nil, cfgs, catalogPath)
	require.NoError(t, err)
	require.NoError(t, results[0].Err)
	require.Equal(t, []string{"gpt-4o"}, results[0].StaleIDs)

	// Stale model must still exist in catalog.
	cat, err := Load(catalogPath)
	require.NoError(t, err)
	require.NotNil(t, cat.Providers["openai"].Models["gpt-4o"], "stale model must be kept in catalog")

	_ = fmt.Sprintf // keep import
}
