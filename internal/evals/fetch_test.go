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
		case "/score.xlsx":
			body = string(syntheticWritingBenchXLSX(t))
		case "/creative_writing.js":
			body = string(syntheticEQBenchCreativeJS())
		case "/lechmazur-writing.md":
			body = writingLeaderboardFixture
		case "/arena-creative.json":
			body = arenaCreativeFixture
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	opts := Options{CacheDir: t.TempDir(), AAAPIKey: "secret", Client: client, Now: func() time.Time { return now }, LLMModelsURL: "https://test/models", LLMFullURL: "https://test/full", LLMIndexURL: "https://test/indexes", AAURL: "https://test/aa", WritingBenchURL: "https://test/score.xlsx", EQBenchCreativeURL: "https://test/creative_writing.js", LechMazurWritingURL: "https://test/lechmazur-writing.md", ArenaCreativeURL: "https://test/arena-creative.json"}
	result, err := Fetch(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("unexpected merged models: %#v", result.Models)
	}
	if len(result.ReferenceModels) != 17 || sourceModelCounts(result.ReferenceModels)["writingbench"] != 1 || sourceModelCounts(result.ReferenceModels)["eqbench_creative_v3"] != 1 || sourceModelCounts(result.ReferenceModels)["ifeval_official"] != 8 {
		t.Fatalf("source-native drift population was not preserved: %#v", result.ReferenceModels)
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
	if len(result.Sources) != 13 || sourceStatusNamed(t, result.Sources, "LLM Stats").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "LLM Stats frozen cohorts").Cache != "unavailable" || sourceStatusNamed(t, result.Sources, "Artificial Analysis").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "WritingBench").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "EQ-Bench Creative v3").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "Lech Mazur Creative Story-Writing").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "Arena Creative Writing").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "Official IFEval").Cache != "frozen" {
		t.Fatalf("unexpected source status: %#v", result.Sources)
	}
	if sourceStatusNamed(t, result.Sources, "WritingBench").ContentSHA == "" || sourceStatusNamed(t, result.Sources, "WritingBench").Methodology == "" || sourceStatusNamed(t, result.Sources, "EQ-Bench Creative v3").CommitSHA == "" || sourceStatusNamed(t, result.Sources, "Official IFEval").Version == "" || sourceStatusNamed(t, result.Sources, "Official IFEval").ContentSHA == "" {
		t.Fatalf("snapshot provenance was not surfaced in source status: %#v", result.Sources)
	}
	firstRequests := requests.Load()
	result, err = Fetch(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != firstRequests {
		t.Fatalf("fresh cache made requests: before=%d after=%d", firstRequests, requests.Load())
	}
	if sourceStatusNamed(t, result.Sources, "LLM Stats").Cache != "fresh" || sourceStatusNamed(t, result.Sources, "LLM Stats frozen cohorts").Cache != "unavailable" || sourceStatusNamed(t, result.Sources, "Artificial Analysis").Cache != "fresh" || sourceStatusNamed(t, result.Sources, "WritingBench").Cache != "fresh" || sourceStatusNamed(t, result.Sources, "EQ-Bench Creative v3").Cache != "fresh" || sourceStatusNamed(t, result.Sources, "Official IFEval").Cache != "frozen" {
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
	official := validOfficialSnapshot(t, "official-lfm25-2.6b")
	official.FetchedAt = now.Add(-30 * 24 * time.Hour)
	if err := writeSnapshot(filepath.Join(dir, "official-lfm25-2.6b.json"), official); err != nil {
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
	if requests.Load() != 0 || len(result.Models) != 3 || len(result.Sources) != 13 || sourceStatusNamed(t, result.Sources, "LLM Stats").Cache != "cached" || sourceStatusNamed(t, result.Sources, "LLM Stats frozen cohorts").Cache != "unavailable" || sourceStatusNamed(t, result.Sources, "Artificial Analysis").Cache != "cached" || sourceStatusNamed(t, result.Sources, "WritingBench").Cache != "unavailable" || sourceStatusNamed(t, result.Sources, "EQ-Bench Creative v3").Cache != "unavailable" || sourceStatusNamed(t, result.Sources, "Lech Mazur Creative Story-Writing").Cache != "unavailable" || sourceStatusNamed(t, result.Sources, "Arena Creative Writing").Cache != "unavailable" || sourceStatusNamed(t, result.Sources, "Official IFEval").Cache != "frozen" || sourceStatusNamed(t, result.Sources, "LiquidAI LFM2.5-2.6B card").Cache != "stale" {
		t.Fatalf("offline fetch touched network or lost cache state: requests=%d result=%#v", requests.Load(), result)
	}
}

func TestFetchRefreshOfficialCardsRequestsOnlyPinnedCards(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	if err := writeSnapshot(filepath.Join(dir, "llm-stats.json"), sourceSnapshot{FetchedAt: now, Models: []Model{{Key: "llm", Name: "LLM", LLMStats: &LLMStatsMetrics{}}}}); err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshot(filepath.Join(dir, "artificial-analysis.json"), sourceSnapshot{FetchedAt: now, Models: []Model{{Key: "aa", Name: "AA", AA: &ArtificialMetrics{}}}}); err != nil {
		t.Fatal(err)
	}
	previousLFM, previousQwen, previousGPT, previousLFMVL, previousGemma := fetchOfficialLFM25Source, fetchOfficialQwen38Source, fetchOfficialGPTOSSSource, fetchOfficialLFMVLSource, fetchOfficialGemmaSource
	t.Cleanup(func() {
		fetchOfficialLFM25Source, fetchOfficialQwen38Source, fetchOfficialGPTOSSSource, fetchOfficialLFMVLSource, fetchOfficialGemmaSource = previousLFM, previousQwen, previousGPT, previousLFMVL, previousGemma
	})
	var requested []string
	fetchOfficialLFM25Source = func(_ context.Context, opts Options) (sourceSnapshot, error) {
		requested = append(requested, opts.OfficialLFMURL)
		return validOfficialSnapshot(t, "official-lfm25-2.6b"), nil
	}
	fetchOfficialQwen38Source = func(_ context.Context, opts Options) (sourceSnapshot, error) {
		requested = append(requested, opts.OfficialQwenURL)
		return validOfficialSnapshot(t, "official-qwen3.8-27b"), nil
	}
	fetchOfficialGPTOSSSource = func(_ context.Context, opts Options) (sourceSnapshot, error) {
		requested = append(requested, opts.OfficialGPTOSSURL)
		return validOfficialSnapshot(t, "official-gpt-oss-20b"), nil
	}
	fetchOfficialLFMVLSource = func(_ context.Context, opts Options) (sourceSnapshot, error) {
		requested = append(requested, opts.OfficialLFMVLURL)
		return validOfficialSnapshot(t, "official-lfm25-vl-3b"), nil
	}
	fetchOfficialGemmaSource = func(_ context.Context, opts Options) (sourceSnapshot, error) {
		requested = append(requested, opts.OfficialGemmaURL)
		return validOfficialSnapshot(t, "official-gemma4"), nil
	}
	var unexpected atomic.Int64
	result, err := Fetch(context.Background(), Options{CacheDir: dir, RefreshOfficialCards: true, Now: func() time.Time { return now }, Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		unexpected.Add(1)
		return nil, fmt.Errorf("non-card source must remain cache-only")
	})}})
	if err != nil {
		t.Fatal(err)
	}
	if unexpected.Load() != 0 || len(requested) != 5 || requested[0] != "https://huggingface.co/LiquidAI/LFM2.5-2.6B/resolve/a334ee78cd38458bb71eda24109ac42dcec1309d/README.md" || requested[1] != "https://huggingface.co/Qwen/Qwen3.8-27B/resolve/1d4bf0f2ff6012fd82039f2fa52739d0dd7c60c0/README.md" || requested[2] != "https://arxiv.org/html/2508.10925v1" || requested[3] != "https://huggingface.co/LiquidAI/LFM2.5-VL-3B/resolve/5a414ead75d45db003906d06fb62bd5b6846cec0/README.md" || requested[4] != "https://huggingface.co/google/gemma-4-31B/resolve/5bbc2fb1c1b2c611d06e3d9f23c170ba21659d89/README.md" {
		t.Fatalf("source-scoped refresh requested unexpected URLs: requests=%#v non-card=%d", requested, unexpected.Load())
	}
	if len(result.Sources) != 13 || sourceStatusNamed(t, result.Sources, "LiquidAI LFM2.5-2.6B card").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "Qwen3.8-27B card").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "OpenAI gpt-oss model card").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "LiquidAI LFM2.5-VL-3B card").Cache != "fetched" || sourceStatusNamed(t, result.Sources, "Google Gemma 4 model card").Cache != "fetched" {
		t.Fatalf("official card refresh did not retain fetched statuses: %#v", result.Sources)
	}
}

func TestFetchRefreshOfficialCardsFailsClosed(t *testing.T) {
	previousLFM, previousQwen, previousGPT, previousLFMVL, previousGemma := fetchOfficialLFM25Source, fetchOfficialQwen38Source, fetchOfficialGPTOSSSource, fetchOfficialLFMVLSource, fetchOfficialGemmaSource
	t.Cleanup(func() {
		fetchOfficialLFM25Source, fetchOfficialQwen38Source, fetchOfficialGPTOSSSource, fetchOfficialLFMVLSource, fetchOfficialGemmaSource = previousLFM, previousQwen, previousGPT, previousLFMVL, previousGemma
	})
	fetchOfficialLFM25Source = func(context.Context, Options) (sourceSnapshot, error) {
		return sourceSnapshot{}, fmt.Errorf("card unavailable")
	}
	if _, err := Fetch(context.Background(), Options{CacheDir: t.TempDir(), RefreshOfficialCards: true}); err == nil || !strings.Contains(err.Error(), "refresh official model card") {
		t.Fatalf("card refresh failure did not terminate fetch: %v", err)
	}
}

func TestFetchOfficialCardRefreshConflictsAreRejected(t *testing.T) {
	for _, opts := range []Options{{Refresh: true, RefreshOfficialCards: true}, {Offline: true, RefreshOfficialCards: true}} {
		if _, err := Fetch(context.Background(), opts); err == nil {
			t.Fatalf("conflicting official-card refresh options were accepted: %#v", opts)
		}
	}
}

func TestMergeRefreshedStatusesMatchesByNameNotOffset(t *testing.T) {
	// Deliberately reorder the card rows relative to refresh order to prove the
	// merge keys on the source name instead of a positional offset.
	result := []SourceStatus{
		{Name: "LLM Stats", Cache: "cached"},
		{Name: "Artificial Analysis", Cache: "cached"},
		{Name: "WritingBench", Cache: "cached"},
		{Name: "EQ-Bench Creative v3", Cache: "cached"},
		{Name: "Official IFEval", Cache: "frozen"},
		{Name: "Google Gemma 4 model card", Cache: "cached"},
		{Name: "Qwen3.8-27B card", Cache: "cached"},
	}
	refreshed := []SourceStatus{
		{Name: "Qwen3.8-27B card", Cache: "fetched", ContentSHA: "qwen-sha"},
		{Name: "Google Gemma 4 model card", Cache: "fetched", ContentSHA: "gemma-sha"},
	}
	if err := mergeRefreshedStatuses(result, refreshed); err != nil {
		t.Fatal(err)
	}
	if result[6].Cache != "fetched" || result[6].ContentSHA != "qwen-sha" {
		t.Fatalf("qwen status landed on the wrong row: %#v", result[6])
	}
	if result[5].Cache != "fetched" || result[5].ContentSHA != "gemma-sha" {
		t.Fatalf("gemma status landed on the wrong row: %#v", result[5])
	}
	for i, status := range result[:5] {
		if status.Cache == "fetched" {
			t.Fatalf("non-card row %d was overwritten: %#v", i, status)
		}
	}
}

func TestMergeRefreshedStatusesFailsClosedOnUnknownSource(t *testing.T) {
	result := []SourceStatus{{Name: "LLM Stats", Cache: "cached"}}
	refreshed := []SourceStatus{{Name: "Unknown future card", Cache: "fetched"}}
	err := mergeRefreshedStatuses(result, refreshed)
	if err == nil || !strings.Contains(err.Error(), "Unknown future card") {
		t.Fatalf("unknown refreshed source did not fail closed: %v", err)
	}
	if result[0].Cache != "cached" {
		t.Fatalf("failed merge mutated the result anyway: %#v", result[0])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
