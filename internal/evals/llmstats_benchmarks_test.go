package evals

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLLMStatsBenchmarkOffsetPaginationSealingAndGradedAdmission(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	offsets := []int{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch request.URL.Path {
		case "/models":
			rows := make([]string, 21)
			for i := range rows {
				rows[i] = fmt.Sprintf(`{"model_id":"m%d","name":"M%d","model_type":"llm","organization":"Lab","is_open":true}`, i, i)
			}
			body = "[" + strings.Join(rows, ",") + "]"
		case "/full":
			body = "[]"
		case "/indexes":
			body = "{}"
		case "/benchmarks":
			body = `[{"benchmark_id":"gpqa","name":"GPQA","max_score":1,"model_count":21}]`
		case "/benchmarks/gpqa":
			offset, _ := strconv.Atoi(request.URL.Query().Get("offset"))
			mu.Lock()
			offsets = append(offsets, offset)
			mu.Unlock()
			count := 20
			if offset == 20 {
				count = 1
			}
			entries := make([]string, count)
			for i := range entries {
				id := offset + i
				entries[i] = fmt.Sprintf(`{"model_id":"m%d","organization_id":"lab","benchmark_score":%d,"verified":false,"self_reported":true}`, id, id)
			}
			body = fmt.Sprintf(`{"benchmark_id":"gpqa","max_score":1,"total_models":21,"entries":[%s]}`, strings.Join(entries, ","))
		default:
			return nil, fmt.Errorf("unexpected request %s", request.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	opts := Options{CacheDir: dir, Refresh: true, IngestLLMBenchmarks: true, Client: client, LLMModelsURL: "https://test/models", LLMFullURL: "https://test/full", LLMIndexURL: "https://test/indexes", LLMBenchmarksURL: "https://test/benchmarks", Now: func() time.Time { return time.Unix(1, 0).UTC() }}
	snapshot, err := fetchLLMStats(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(offsets) != "[0 20]" {
		t.Fatalf("offsets = %v", offsets)
	}
	if len(snapshot.Models) != 21 || snapshot.Models[0].Benchmarks["gpqa"].EvidenceGrade != "aggregator_self_reported" {
		t.Fatalf("graded rows were not attached: %#v", snapshot.Models[0])
	}
	statsCohorts, err := loadFrozenLLMStatsStatsV1Cohorts()
	if err != nil {
		t.Fatal(err)
	}
	wantPercentile := empiricalPercentile(statsCohorts.Benchmarks["gpqa"].Scores, 0, true)
	if score := scoreCategoryV3(snapshot.Models[0], categorySpecs[4], snapshot.Models); score == nil || math.Abs(score.Coverage-.25) > 1e-12 || math.Abs(score.TrustedCoverage-.125) > 1e-12 || math.Abs(score.Score-wantPercentile) > 1e-12 || score.Confidence != "low" {
		t.Fatalf("graded LLM Stats rows did not use the raw common cohort: %#v", score)
	}
	for _, pattern := range []string{filepath.Join(dir, "sealed", "llm-stats", "models-*.json"), filepath.Join(dir, "sealed", "llm-stats", "full-results-*.json"), filepath.Join(dir, "sealed", "llm-stats", "indexes-*.json"), filepath.Join(dir, "sealed", "llm-stats", "catalog-*.json"), filepath.Join(dir, "sealed", "llm-stats", "observations-*.jsonl"), filepath.Join(dir, "llm-stats-observations.sqlite")} {
		matches, _ := filepath.Glob(pattern)
		path := pattern
		if len(matches) == 1 {
			path = matches[0]
		}
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("missing sealed/index artifact %s: %v", path, err)
		}
	}
}

func TestLLMStatsBenchmarkPaginationRejectsIncompletePage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"benchmark_id":"gpqa","total_models":2,"entries":[]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	opts := Options{CacheDir: t.TempDir(), Client: client, LLMBenchmarksURL: "https://test/benchmarks", Now: time.Now}
	_, err := fetchLLMStatsBenchmarkPages(context.Background(), opts, filepath.Join(opts.CacheDir, "sealed"), llmStatsBenchmark{ID: "gpqa", Name: "GPQA", ModelCount: 2})
	if err == nil || !strings.Contains(err.Error(), "pagination incomplete") {
		t.Fatalf("expected incomplete pagination error, got %v", err)
	}
}

func TestLLMStatsBenchmarkCatalogRejectsSchemaDrift(t *testing.T) {
	for _, input := range []string{`[]`, `[{"name":"missing id"}]`, `[{"benchmark_id":1,"name":"bad type"}]`} {
		if _, err := decodeLLMStatsBenchmarkCatalog([]byte(input)); err == nil {
			t.Fatalf("schema drift accepted: %s", input)
		}
	}
}
