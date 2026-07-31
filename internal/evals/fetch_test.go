package evals

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchSourcesAndFreshCache(t *testing.T) {
	var requests atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		var body string
		switch r.URL.Path {
		case "/models":
			body = `[{"model_id":"gpt-oss-20b-high","name":"GPT OSS 20B High","model_type":"llm","organization":"Lab","license":"apache_2_0","is_open":true},{"model_id":"closed-llm","name":"Closed LLM","model_type":"llm","organization":"Lab","is_open":false},{"model_id":"not-an-llm","name":"Image","model_type":"image","is_open":false}]`
		case "/full":
			body = `[{"model_id":"gpt-oss-20b-high","gpqa_score":0.8,"swe_bench_verified_score":0.7}]`
		case "/indexes":
			body = `{"coding":{"models":[{"model_id":"gpt-oss-20b-high","conservative":42,"mu":44,"sigma":1,"rank":2,"games_played":5}]}}`
		case "/aa":
			if r.Header.Get("x-api-key") != "secret" {
				t.Errorf("missing API key")
			}
			body = `{"intelligence_index_version":4.1,"pagination":{"page":1,"total_pages":1,"has_more":false},"data":[{"id":"aa-1","name":"GPT OSS 20B High","slug":"gpt-oss-20b","model_creator":{"name":"Lab"},"evaluations":{"artificial_analysis_intelligence_index":30,"artificial_analysis_coding_index":25},"pricing":{"price_1m_input_tokens":1},"performance":{"median_output_tokens_per_second":100}}]}`
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	opts := Options{CacheDir: t.TempDir(), AAAPIKey: "secret", Client: client, Now: func() time.Time { return now }, LLMModelsURL: "https://test/models", LLMFullURL: "https://test/full", LLMIndexURL: "https://test/indexes", AAURL: "https://test/aa"}
	result, err := Fetch(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("unexpected merged models: %#v", result.Models)
	}
	models := make(map[string]Model, len(result.Models))
	for _, model := range result.Models {
		models[model.Key] = model
	}
	if models["gpt-oss-20b-high"].AA == nil || models["gpt-oss-20b-high"].LLMStats == nil {
		t.Fatalf("safe cross-source variant was not merged: %#v", models["gpt-oss-20b-high"])
	}
	if models["gpt-oss-20b-high"].IdentityMatch != IdentityMatchNormalized || models["closed-llm"].IdentityMatch != IdentityMatchUnmatched {
		t.Fatalf("unexpected identity status: %#v", models)
	}
	closed := models["closed-llm"]
	if closed.Open == nil || *closed.Open {
		t.Fatalf("closed LLM was not retained: %#v", closed)
	}
	if len(result.Sources) != 2 || result.Sources[0].Cache != "fetched" || result.Sources[1].Cache != "fetched" {
		t.Fatalf("unexpected source status: %#v", result.Sources)
	}
	firstRequests := requests.Load()
	result, err = Fetch(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != firstRequests {
		t.Fatalf("fresh cache made requests: before=%d after=%d", firstRequests, requests.Load())
	}
	if result.Sources[0].Cache != "fresh" || result.Sources[1].Cache != "fresh" {
		t.Fatalf("expected fresh cache: %#v", result.Sources)
	}
}

func TestFetchUsesStaleCacheAndAllowsMissingAAKey(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	if err := writeSnapshot(filepath.Join(dir, "llm-stats.json"), sourceSnapshot{FetchedAt: now.Add(-72 * time.Hour), Models: []Model{{Key: "cached", Name: "Cached", LLMStats: &LLMStatsMetrics{}}}}); err != nil {
		t.Fatal(err)
	}
	opts := Options{CacheDir: dir, Refresh: true, AllowPartial: true, Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("offline") })}, Now: func() time.Time { return now }}
	result, err := Fetch(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sources[0].Cache != "stale" || result.Sources[1].Cache != "unavailable" {
		t.Fatalf("unexpected status: %#v", result.Sources)
	}
	if len(result.Models) != 1 {
		t.Fatalf("unexpected models: %#v", result.Models)
	}
}

func TestMergeModelsMatchesUniqueNormalizedIdentity(t *testing.T) {
	llm := []Model{{Key: "model", Name: "Model 3.1-Pro", Organization: "Lab", LLMStats: &LLMStatsMetrics{}}}
	aa := []Model{{Key: "model-3-1-pro", Name: "Model 3.1 Pro", Organization: "Lab", AA: &ArtificialMetrics{}}}
	merged := mergeModels(llm, aa)
	if len(merged) != 1 || merged[0].AA == nil || merged[0].LLMStats == nil || merged[0].IdentityMatch != IdentityMatchNormalized {
		t.Fatalf("unique normalized identity was not merged: %#v", merged)
	}
}

func TestMergeModelsMatchesReviewedOrganizationAliases(t *testing.T) {
	llm := []Model{
		{Key: "qwen", Name: "Qwen 3.6-27B", Organization: "Alibaba Cloud / Qwen Team", LLMStats: &LLMStatsMetrics{}},
	}
	aa := []Model{{Key: "qwen-3-6-27b", Name: "Qwen 3.6 27B", Organization: "Alibaba", AA: &ArtificialMetrics{Slug: "qwen-3-6-27b"}}}
	merged := mergeModels(llm, aa)
	if len(merged) != 1 || merged[0].AA == nil || merged[0].LLMStats == nil || merged[0].IdentityMatch != IdentityMatchNormalized {
		t.Fatalf("reviewed organization aliases were not merged: %#v", merged)
	}
}

func TestMergeModelsMarksMatchingSourceKeysExact(t *testing.T) {
	llm := []Model{{Key: "model-3.1-pro", Name: "Model 3.1-Pro", Organization: "Lab", LLMStats: &LLMStatsMetrics{}}}
	aa := []Model{{Key: "model-3-1-pro", Name: "Model 3.1 Pro", Organization: "Lab", AA: &ArtificialMetrics{}}}
	merged := mergeModels(llm, aa)
	if len(merged) != 1 || merged[0].IdentityMatch != IdentityMatchExact {
		t.Fatalf("matching source keys were not exact: %#v", merged)
	}
}

func TestMergeModelsKeepsAmbiguousDuplicatesSeparate(t *testing.T) {
	llm := []Model{
		{Key: "one", Name: "Model One", Organization: "Lab", LLMStats: &LLMStatsMetrics{}},
		{Key: "two", Name: "Model One", Organization: "Lab", LLMStats: &LLMStatsMetrics{}},
	}
	aa := []Model{{Key: "model-one", Name: "Model One", Organization: "Lab", AA: &ArtificialMetrics{}}}
	merged := mergeModels(llm, aa)
	if len(merged) != 3 || merged[0].AA != nil || merged[1].AA != nil {
		t.Fatalf("ambiguous identity was merged: %#v", merged)
	}
	for _, model := range merged {
		if model.IdentityMatch != IdentityMatchAmbiguous {
			t.Fatalf("ambiguous row was not labeled: %#v", merged)
		}
	}
}

func TestMergeModelsPreservesNameQualifiers(t *testing.T) {
	llm := []Model{{Key: "command-a", Name: "Command A", Organization: "Cohere", LLMStats: &LLMStatsMetrics{}}}
	aa := []Model{{Key: "command-a-plus", Name: "Command A+", Organization: "Cohere", AA: &ArtificialMetrics{}}}
	merged := mergeModels(llm, aa)
	if len(merged) != 2 || merged[0].AA != nil || merged[0].IdentityMatch != IdentityMatchUnmatched || merged[1].IdentityMatch != IdentityMatchUnmatched {
		t.Fatalf("qualified variants collided: %#v", merged)
	}
}

func TestFetchRequiresArtificialAnalysisByDefault(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	if err := writeSnapshot(filepath.Join(dir, "llm-stats.json"), sourceSnapshot{FetchedAt: now, Models: []Model{{Key: "cached", Name: "Cached", LLMStats: &LLMStatsMetrics{}}}}); err != nil {
		t.Fatal(err)
	}
	_, err := Fetch(context.Background(), Options{CacheDir: dir, Now: func() time.Time { return now }})
	if err == nil || !strings.Contains(err.Error(), "AA_API_KEY") {
		t.Fatalf("expected missing Artificial Analysis key error, got %v", err)
	}
}

func TestFetchOfflineUsesOnlyExistingSnapshots(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	if err := writeSnapshot(filepath.Join(dir, "llm-stats.json"), sourceSnapshot{FetchedAt: now.Add(-30 * 24 * time.Hour), Models: []Model{{Key: "llm", Name: "LLM", LLMStats: &LLMStatsMetrics{}}}}); err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshot(filepath.Join(dir, "artificial-analysis.json"), sourceSnapshot{FetchedAt: now.Add(-30 * 24 * time.Hour), AAVersion: 4.1, Models: []Model{{Key: "aa", Name: "AA", AA: &ArtificialMetrics{}}}}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	result, err := Fetch(context.Background(), Options{
		CacheDir: dir, Offline: true, Now: func() time.Time { return now },
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return nil, fmt.Errorf("network must not be used")
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 || len(result.Models) != 2 || result.Sources[0].Cache != "cached" || result.Sources[1].Cache != "cached" {
		t.Fatalf("offline fetch touched network or lost cache state: requests=%d result=%#v", requests.Load(), result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
