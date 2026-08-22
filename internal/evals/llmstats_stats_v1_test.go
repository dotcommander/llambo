package evals

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLLMStatsStatsV1BearerPaginationDedupeNormalizationAndSealing(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 22, 14, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing Bearer token: %q", request.Header.Get("Authorization"))
		}
		body := ""
		switch request.URL.Path {
		case "/stats/v1/benchmarks":
			body = `{"benchmarks":[{"id":"unreviewed","name":"Unreviewed","model_count":99},{"id":"gpqa","name":"GPQA","model_count":5}]}`
		case "/stats/v1/scores":
			if request.URL.Query().Get("benchmark") != "gpqa" || request.URL.Query().Get("limit") != "500" {
				t.Fatalf("unexpected scores query: %s", request.URL.RawQuery)
			}
			if request.URL.Query().Get("cursor") == "" {
				body = fmt.Sprintf(`{"scores":[%s,%s,%s],"next_cursor":"cursor-2","total":6}`, statsV1ScoreRow("model-a", .946, true), statsV1ScoreRow("model-a", .946, true), statsV1ScoreRow("model-b", 0, false))
			} else if request.URL.Query().Get("cursor") == "cursor-2" {
				body = fmt.Sprintf(`{"scores":[%s,%s,%s,%s],"next_cursor":"","total":6}`, statsV1ScoreRow("model-c", .25, true), statsV1ScoreRow("model-d", .75, true), statsV1ScoreRow("model-e", .6, false), statsV1ScoreRow("model-a", .946, true))
			} else {
				t.Fatalf("unexpected cursor %q", request.URL.Query().Get("cursor"))
			}
		default:
			return nil, fmt.Errorf("unexpected request %s", request.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	opts := Options{
		CacheDir: dir, LLMStatsAPIKey: "secret", Client: client,
		LLMStatsStatsV1BenchmarksURL: "https://test/stats/v1/benchmarks",
		LLMStatsStatsV1ScoresURL:     "https://test/stats/v1/scores",
		Now:                          func() time.Time { return now },
	}
	observations, err := fetchLLMStatsStatsV1BenchmarkLeads(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 5 {
		t.Fatalf("duplicate rows were not removed: %#v", observations)
	}
	byModel := make(map[string]Observation, len(observations))
	for _, observation := range observations {
		byModel[observation.ModelID] = observation
		if err := observation.Validate(); err != nil {
			t.Fatalf("observation %s invalid: %v", observation.ModelID, err)
		}
		if observation.SourceID != llmStatsStatsV1SourceID || observation.BenchmarkVersion != "stats-v1" || observation.Unit != "percent" {
			t.Fatalf("unexpected Stats v1 provenance: %#v", observation)
		}
	}
	if byModel["model-a"].RawScore != 94.6 || byModel["model-b"].RawScore != 0 || byModel["model-c"].RawScore != 25 || byModel["model-d"].RawScore != 75 || byModel["model-e"].RawScore != 60 {
		t.Fatalf("normalized scores are wrong: %#v", byModel)
	}
	if byModel["model-a"].EvidenceGrade != "aggregator_self_reported" || byModel["model-b"].EvidenceGrade != "aggregator_unspecified" {
		t.Fatalf("evidence grades are wrong: %#v", byModel)
	}
	assertStatsV1SealedArtifacts(t, dir)
}

func TestLLMStatsStatsV1RejectsConflictingDuplicateModels(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch request.URL.Path {
		case "/stats/v1/benchmarks":
			body = `{"benchmarks":[{"id":"gpqa","name":"GPQA","model_count":5}]}`
		case "/stats/v1/scores":
			body = fmt.Sprintf(`{"scores":[%s,%s],"next_cursor":"","total":2}`, statsV1ScoreRow("model-a", .9, true), statsV1ScoreRow("model-a", .8, true))
		default:
			return nil, fmt.Errorf("unexpected request %s", request.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	opts := Options{
		CacheDir: t.TempDir(), LLMStatsAPIKey: "secret", Client: client,
		LLMStatsStatsV1BenchmarksURL: "https://test/stats/v1/benchmarks",
		LLMStatsStatsV1ScoresURL:     "https://test/stats/v1/scores",
		Now:                          time.Now,
	}
	_, err := fetchLLMStatsStatsV1BenchmarkLeads(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "conflicting scores") {
		t.Fatalf("expected conflicting duplicate failure, got %v", err)
	}
}

func TestLLMStatsStatsV1RowsUsePinnedCommonCohortScale(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsStatsV1Cohorts()
	if err != nil {
		t.Fatal(err)
	}
	cohort := cohorts.Benchmarks["gpqa"]
	score := cohort.Scores[0]
	model := Model{Key: "model-a", Benchmarks: map[string]BenchmarkResult{
		"gpqa": {
			Score: &score, Identity: IdentityMatchExact, Version: llmStatsStatsV1BenchmarkVersion,
			ContentSHA: cohort.Digests[0], Method: llmStatsStatsV1Methodology,
			SourceClass: string(SourceAggregatorResult), EvidenceGrade: "aggregator_self_reported",
			Direction: "higher", Cohort: llmStatsStatsV1Cohort, SourceID: llmStatsStatsV1SourceID,
			SourceRevision: cohort.Digests[0],
		},
	}}
	result := scoreCategoryV3(model, categorySpecNamed(t, "reasoning"), []Model{model})
	want := empiricalPercentile(cohort.Scores, score, true)
	if result == nil || result.Primary == nil || result.Primary.RawScore == nil || *result.Primary.RawScore != score || result.Primary.ReferencePopulation != cohort.RowCount || result.Primary.SourceRevision != cohort.Digests[0] || result.Score != want {
		t.Fatalf("Stats v1 row did not use its pinned frozen cohort: %#v", result)
	}
	model.Benchmarks["gpqa"] = BenchmarkResult{
		Score: &score, Identity: IdentityMatchExact, Version: llmStatsStatsV1BenchmarkVersion,
		ContentSHA: strings.Repeat("f", 64), Method: llmStatsStatsV1Methodology,
		SourceClass: string(SourceAggregatorResult), EvidenceGrade: "aggregator_self_reported",
		Direction: "higher", Cohort: llmStatsStatsV1Cohort, SourceID: llmStatsStatsV1SourceID,
		SourceRevision: strings.Repeat("f", 64),
	}
	if result := scoreCategoryV3(model, categorySpecNamed(t, "reasoning"), []Model{model}); result == nil || result.Score != want || result.Primary.ReferencePopulation != cohort.RowCount {
		t.Fatalf("unpinned Stats v1 revision did not use the pinned common scale: %#v", result)
	}
}

func TestCommonCohortAppliesToFirstPartyRawResults(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsStatsV1Cohorts()
	if err != nil {
		t.Fatal(err)
	}
	cohort := cohorts.Benchmarks["bfcl-v4"]
	value := 56.88
	result := BenchmarkResult{
		Score: &value, Identity: IdentityMatchExact, Version: "official-card-revision",
		ContentSHA: strings.Repeat("a", 64), Method: "published comparison table",
		SourceClass: string(SourceFirstPartyResult), EvidenceGrade: "first_party",
		Direction: "higher", Cohort: "official-comparison-table",
		SourceID: "liquidai-lfm25-2.6b-card", SourceRevision: "official-card-revision",
	}
	model := Model{Benchmarks: map[string]BenchmarkResult{"bfcl-v4": result}}
	score := scoreCategoryV3(model, categorySpecNamed(t, "agents"), []Model{model})
	want := empiricalPercentile(cohort.Scores, value, true)
	if score == nil || score.Score != want || score.Primary.ReferencePopulation != cohort.RowCount || score.TrustedCoverage != .9/3 {
		t.Fatalf("first-party raw result did not use the broad common cohort: %#v", score)
	}
}

func TestLLMStatsStatsV1CatalogRejectsSchemaDrift(t *testing.T) {
	for _, input := range []string{`{}`, `{"benchmarks":[]}`, `{"benchmarks":[{"id":"","name":"GPQA","model_count":1}]}`, `{"benchmarks":[{"id":"gpqa","name":"GPQA","model_count":-1}]}`} {
		if _, err := decodeLLMStatsStatsV1Benchmarks([]byte(input)); err == nil {
			t.Fatalf("schema drift accepted: %s", input)
		}
	}
}

func TestLLMStatsStatsV1OptionsDefaults(t *testing.T) {
	var options Options
	options.applyDefaults()
	if options.LLMStatsStatsV1BenchmarksURL != LLMStatsStatsV1BenchmarksURL || options.LLMStatsStatsV1ScoresURL != LLMStatsStatsV1ScoresURL {
		t.Fatalf("unexpected Stats v1 defaults: %#v", options)
	}
	if options.LLMStatsStatsV1ModelsURL != LLMStatsStatsV1ModelsURL {
		t.Fatalf("unexpected Stats v1 model default: %q", options.LLMStatsStatsV1ModelsURL)
	}
}

func TestLLMStatsStatsV1FrozenCohortManifestIsPinned(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsStatsV1Cohorts()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"arena-hard": 26, "bfcl-v4": 15, "gpqa": 239, "humaneval": 66, "ifbench": 34,
		"ifeval": 67, "livecodebench": 75, "livecodebench-v6": 56, "longbench-v2": 17,
		"math": 71, "mbpp": 33, "mmlu-pro": 134, "multi-if": 23, "multipl-e": 13,
		"osworld": 20, "scicode": 21, "swe-bench-pro": 50, "swe-bench-verified": 111,
		"tau-bench-retail": 25, "tau3-bench": 5, "writingbench": 15,
	}
	if len(cohorts.Benchmarks) != len(want) {
		t.Fatalf("frozen Stats v1 benchmark count = %d, want %d", len(cohorts.Benchmarks), len(want))
	}
	for benchmark, population := range want {
		if cohorts.Benchmarks[benchmark].RowCount != population || len(cohorts.Benchmarks[benchmark].Scores) != population {
			t.Fatalf("frozen Stats v1 cohort %s population = %d, want %d", benchmark, cohorts.Benchmarks[benchmark].RowCount, population)
		}
	}
}

func TestLLMStatsStatsV1FallsBackToScoreOverMaxScore(t *testing.T) {
	row := llmStatsStatsV1ScoreRow{
		ModelID: "model", ModelName: "Model", Organization: "lab", BenchmarkID: "gpqa",
		BenchmarkName: "GPQA", Score: .5, MaxScore: 1, ScoredAt: time.Unix(1, 0).UTC(),
		Source: "LLM Stats", URL: "https://llm-stats.com/models/model",
	}
	observation, err := llmStatsStatsV1Observation(row, llmStatsStatsV1Benchmark{ID: "gpqa", ModelCount: 1}, strings.Repeat("a", 64), "https://test/scores", 0, func() time.Time { return time.Unix(2, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	if observation.RawScore != 50 {
		t.Fatalf("score/max fallback did not normalize to percent: %#v", observation)
	}
}

func statsV1ScoreRow(modelID string, normalized float64, selfReported bool) string {
	normalizedValue := fmt.Sprintf("%g", normalized)
	if normalized < 0 {
		normalizedValue = "null"
	}
	selfReportedValue := "false"
	if selfReported {
		selfReportedValue = "true"
	}
	return fmt.Sprintf(`{"model_id":%q,"model_name":%q,"organization":"lab","benchmark_id":"gpqa","benchmark_name":"GPQA","category":"reasoning","score":%g,"normalized_score":%s,"max_score":1,"is_self_reported":%s,"verified":false,"scored_at":"2026-08-22T13:00:00Z","source":"LLM Stats","url":"https://llm-stats.com/models/%s"}`, modelID, strings.ToUpper(modelID), normalized, normalizedValue, selfReportedValue, modelID)
}

func assertStatsV1SealedArtifacts(t *testing.T, dir string) {
	t.Helper()
	for _, pattern := range []string{
		filepath.Join(dir, "sealed", "llm-stats-stats-v1", "catalog-*.json"),
		filepath.Join(dir, "sealed", "llm-stats-stats-v1", "blocked-*.jsonl"),
		filepath.Join(dir, "sealed", "llm-stats-stats-v1", "observations-*.jsonl"),
		filepath.Join(dir, "llm-stats-stats-v1-observations.sqlite"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) != 1 {
			t.Fatalf("expected one sealed artifact for %s: matches=%#v err=%v", pattern, matches, err)
		}
		if info, err := os.Stat(matches[0]); err != nil || info.Size() == 0 {
			t.Fatalf("empty sealed artifact %s: %v", matches[0], err)
		}
	}
	pages, _ := filepath.Glob(filepath.Join(dir, "sealed", "llm-stats-stats-v1", "*", "*.json"))
	if len(pages) != 2 {
		t.Fatalf("expected two sealed score pages: %#v", pages)
	}
}
