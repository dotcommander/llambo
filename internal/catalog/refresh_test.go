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

func TestOpenAICompatFetcherPreservesEmptyBaseEndpoint(t *testing.T) {
	t.Parallel()
	_, endpoint, err := (openaiCompatFetcher{}).Fetch(context.Background(), providers.Config{ProviderType: "openai", RequiresKey: false})
	require.Error(t, err)
	require.Equal(t, "/v1/models", endpoint)
}

func TestBuildOpenAIModelsRequestSpec(t *testing.T) {
	t.Parallel()

	spec := BuildOpenAIModelsRequestSpec("https://models.example/api", "catalog-key", map[string]string{
		"accept":        "application/vnd.models+json",
		"aUtHoRiZaTiOn": "Bearer gateway-key",
		"X-Gateway":     "catalog",
	})

	require.Equal(t, "https://models.example/api/v1/models", spec.Endpoint)
	require.Equal(t, map[string]string{
		"Accept":        "application/vnd.models+json",
		"Authorization": "Bearer gateway-key",
		"X-Gateway":     "catalog",
	}, spec.Headers)
}

func TestDecodeOpenAICompatibleModelsPreservesMetadataAndOrder(t *testing.T) {
	t.Parallel()

	models, err := DecodeOpenAICompatibleModels([]byte(`{
		"data": [
			{
				"id": "second",
				"canonical_slug": "vendor/second",
				"name": "Second model",
				"created": 1700000000,
				"owned_by": "vendor",
				"context_length": 128000,
				"architecture": {"modality": "text->text", "input_modalities": ["text"], "output_modalities": ["text"], "tokenizer": "test", "instruct_type": "chat"},
				"pricing": {"prompt": "0.1", "completion": "0.2", "image": "0.3", "audio": "0.4", "web_search": "0.5", "internal_reasoning": "0.6", "input_cache_read": "0.7", "input_cache_write": "0.8"},
				"top_provider": {"context_length": 64000, "max_completion_tokens": 4096, "is_moderated": true},
				"supported_parameters": ["max_tokens", "tools"],
				"default_parameters": {"temperature": 1},
				"reasoning": {"enabled": true},
				"benchmarks": {"mmlu": 0.9}
			},
			{"id": ""},
			{"id": "first", "owned_by": "other"}
		]
	}`))
	require.NoError(t, err)
	require.Len(t, models, 2)
	require.Equal(t, []string{"second", "first"}, []string{models[0].ID, models[1].ID})
	require.Equal(t, "vendor", models[0].OwnedBy)
	require.Equal(t, time.Unix(1700000000, 0).UTC(), models[0].UpstreamCreated)
	require.Equal(t, ModelMetadata{
		Name:                "Second model",
		CanonicalSlug:       "vendor/second",
		ContextLength:       128000,
		SupportedParameters: []string{"max_tokens", "tools"},
		DefaultParameters:   map[string]any{"temperature": float64(1)},
		Pricing: ModelPricing{
			Prompt: "0.1", Completion: "0.2", Image: "0.3", Audio: "0.4",
			WebSearch: "0.5", InternalReasoning: "0.6", InputCacheRead: "0.7", InputCacheWrite: "0.8",
		},
		Architecture: ModelArchitecture{
			Modality: "text->text", InputModalities: []string{"text"}, OutputModalities: []string{"text"}, Tokenizer: "test", InstructType: "chat",
		},
		TopProvider: ModelTopProvider{ContextLength: 64000, MaxCompletionTokens: 4096, IsModerated: true},
		Reasoning:   map[string]any{"enabled": true},
		Benchmarks:  map[string]any{"mmlu": 0.9},
	}, models[0].Metadata)
	require.Equal(t, "other", models[1].OwnedBy)
}

func TestDecodeOpenAICompatibleModelsRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()

	_, err := DecodeOpenAICompatibleModels([]byte(`{"data":[{"id":"usable","architecture":"not-an-object"}]}`))
	require.Error(t, err)
}

func TestDecodeGeminiModelsPreservesMetadataOrderAndOnePrefix(t *testing.T) {
	t.Parallel()

	models, err := DecodeGeminiModels([]byte(`{
		"models": [
			{
				"name": "models/second",
				"displayName": "Second model",
				"description": "Second description",
				"inputTokenLimit": 128000,
				"outputTokenLimit": 8192,
				"supportedGenerationMethods": ["generateContent", "countTokens"]
			},
			{"name": "models/models/first"},
			{"name": "models/"}
		]
	}`))
	require.NoError(t, err)
	require.Equal(t, []UpstreamModel{
		{
			ID: "second",
			Metadata: ModelMetadata{
				Name:                       "Second model",
				Description:                "Second description",
				ContextLength:              128000,
				InputTokenLimit:            128000,
				OutputTokenLimit:           8192,
				SupportedGenerationMethods: []string{"generateContent", "countTokens"},
			},
		},
		{ID: "models/first"},
	}, models)
}

func TestDecodeGeminiModelsRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()

	_, err := DecodeGeminiModels([]byte(`{"models":[{"name":"models/usable","supportedGenerationMethods":"not-an-array"}]}`))
	require.Error(t, err)
}

func TestAPIKeyUsesFirstConfiguredArrayKey(t *testing.T) {
	t.Parallel()

	key, err := apiKey("catalog", providers.Config{
		APIKeys:     []string{"", "catalog-array-key"},
		RequiresKey: true,
	})
	require.NoError(t, err)
	require.Equal(t, "catalog-array-key", key)
}

func TestAPIKeyPreservesRequiredKeyError(t *testing.T) {
	t.Parallel()

	key, err := apiKey("catalog", providers.Config{RequiresKey: true})
	require.Empty(t, key)
	require.EqualError(t, err, "no API key configured for provider \"catalog\" (set api_key, api_keys, or )")
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

func TestRefreshWithOptions_CachingAndIdempotency(t *testing.T) {
	t.Parallel()

	hitCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data": [{"id": "model-v1", "owned_by": "test"}]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")

	seed := &Catalog{Version: 1, Providers: make(map[string]*ProviderCatalog)}
	require.NoError(t, Save(catalogPath, seed))

	cfgs := map[string]providers.Config{
		"testprov": {
			BaseURL:      srv.URL,
			ProviderType: "openai",
			RequiresKey:  false,
			Enabled:      true,
			Model:        "model-v1",
		},
	}

	// 1. Initial refresh with TTL 4 hours: hits server
	res1, err := RefreshWithOptions(context.Background(), nil, cfgs, catalogPath, RefreshOptions{TTL: 4 * time.Hour})
	require.NoError(t, err)
	require.Len(t, res1, 1)
	require.False(t, res1[0].Cached)
	require.Equal(t, 1, hitCount)
	require.Equal(t, 1, res1[0].Total)
	require.Contains(t, res1[0].NewIDs, "model-v1")
	require.NoError(t, Update(context.Background(), catalogPath, func(cat *Catalog) error {
		cat.Providers["testprov"].Models["stale"] = &ModelEntry{LastSeen: time.Unix(1, 0)}
		return nil
	}))

	// 2. Second refresh within 4 hours: idempotent, served from cache, does NOT hit server
	res2, err := RefreshWithOptions(context.Background(), nil, cfgs, catalogPath, RefreshOptions{TTL: 4 * time.Hour})
	require.NoError(t, err)
	require.Len(t, res2, 1)
	require.True(t, res2[0].Cached)
	require.Equal(t, 1, hitCount, "must not make network call when cached within 4h")
	require.Equal(t, 1, res2[0].Total)

	// 3. Forced refresh: bypasses cache and hits server
	res3, err := RefreshWithOptions(context.Background(), nil, cfgs, catalogPath, RefreshOptions{TTL: 4 * time.Hour, Force: true})
	require.NoError(t, err)
	require.Len(t, res3, 1)
	require.False(t, res3[0].Cached)
	require.Equal(t, 2, hitCount, "must hit server when Force is true")
}

func TestRefresh_UpdatesVersionAndMetadataOfTrackedAndScoredModel(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"data": [
				{
					"id": "model-tracked",
					"owned_by": "openai",
					"created": 1750000000,
					"context_length": 1048576,
					"description": "Updated model version"
				},
				{
					"id": "model-brand-new",
					"owned_by": "openai",
					"created": 1750000100,
					"context_length": 262144
				}
			]
		}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")

	oldTime := now.Add(-5 * time.Hour)
	seed := &Catalog{
		Version: 1,
		Providers: map[string]*ProviderCatalog{
			"openai": {
				LastRefresh: oldTime,
				Models: map[string]*ModelEntry{
					"model-tracked": {
						FirstSeen: oldTime,
						LastSeen:  oldTime,
						OwnedBy:   "old-owner",
						Metadata: ModelMetadata{
							ContextLength: 128000,
							Description:   "Old version",
						},
						Quality: map[string]QualityEvidence{
							"overall": {Score: 0.88, Source: "artificial_analysis"},
						},
						Benchmarks: map[string]BenchmarkEvidence{
							"speed": {SpeedTokensPerSecond: 120.5},
						},
						Pinned: true,
						Tags:   []string{"smart"},
					},
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
			Model:        "model-tracked",
		},
	}

	results, err := RefreshWithOptions(context.Background(), nil, cfgs, catalogPath, RefreshOptions{TTL: 4 * time.Hour})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, 1, results[0].UpdatedN, "tracked model must be counted as updated")
	require.Equal(t, []string{"model-brand-new"}, results[0].NewIDs, "brand new model must be identified")

	cat, err := Load(catalogPath)
	require.NoError(t, err)

	// 1. Verify tracked & scored model updated its version/metadata while preserving scores/policy
	tracked := cat.Providers["openai"].Models["model-tracked"]
	require.NotNil(t, tracked)
	require.Equal(t, 1048576, tracked.Metadata.ContextLength, "context length must update")
	require.Equal(t, "Updated model version", tracked.Metadata.Description, "description must update")
	require.Equal(t, "openai", tracked.OwnedBy, "owner must update")
	require.True(t, tracked.Pinned, "pinned state must be preserved")
	require.Equal(t, []string{"smart"}, tracked.Tags, "tags must be preserved")
	require.NotNil(t, tracked.Quality["overall"], "quality score must be preserved")
	require.Equal(t, 0.88, tracked.Quality["overall"].Score, "quality score value must be preserved")
	require.Equal(t, 120.5, tracked.Benchmarks["speed"].SpeedTokensPerSecond, "benchmark metrics must be preserved")

	// 2. Verify new model is tracked
	newModel := cat.Providers["openai"].Models["model-brand-new"]
	require.NotNil(t, newModel)
	require.Equal(t, 262144, newModel.Metadata.ContextLength)
}

func TestRefreshMetadataChangeDetection(t *testing.T) {
	now := time.Now().UTC()
	cat := &Catalog{Providers: map[string]*ProviderCatalog{}}
	fetch := refreshFetch{result: RefreshResult{Provider: "test"}, upstream: []UpstreamModel{{ID: "model", Metadata: ModelMetadata{
		Description: "original", SupportedParameters: []string{"temperature"},
		DefaultParameters: map[string]any{"temperature": 0.5},
		Architecture:      ModelArchitecture{InputModalities: []string{"text"}},
	}}}}
	mergeRefresh(cat, fetch, now)
	got := mergeRefresh(cat, fetch, now.Add(time.Minute))
	require.Equal(t, 0, got.UpdatedN)
	require.Equal(t, 1, got.UnchangedN)
	fetch.upstream[0].Metadata = ModelMetadata{Description: "changed"}
	got = mergeRefresh(cat, fetch, now.Add(2*time.Minute))
	require.Equal(t, 1, got.UpdatedN)
	require.Equal(t, []string{"temperature"}, cat.Providers["test"].Models["model"].Metadata.SupportedParameters)
	require.Equal(t, "changed", cat.Providers["test"].Models["model"].Metadata.Description)
}
