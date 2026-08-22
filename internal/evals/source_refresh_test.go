package evals

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRefreshEvaluationSourcesIsScopedAndValidatesProvenance(t *testing.T) {
	var writingRequests, eqBenchRequests, lechRequests int
	writing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writingRequests++
		w.Header().Set("ETag", `"wb-etag"`)
		w.Header().Set("X-Repo-Commit", "wb-commit")
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		_, _ = w.Write(syntheticWritingBenchXLSX(t))
	}))
	defer writing.Close()
	eqBench := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eqBenchRequests++
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(syntheticEQBenchCreativeJS()))
	}))
	defer eqBench.Close()
	lech := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lechRequests++
		w.Header().Set("ETag", `"lech-revision"`)
		_, _ = w.Write([]byte(writingLeaderboardFixture))
	}))
	defer lech.Close()

	now := time.Unix(1, 0).UTC()
	dir := t.TempDir()
	statuses, _, err := RefreshEvaluationSources(context.Background(), Options{
		CacheDir:            dir,
		Client:              &http.Client{},
		Now:                 func() time.Time { return now },
		WritingBenchURL:     writing.URL,
		EQBenchCreativeURL:  eqBench.URL,
		LechMazurWritingURL: lech.URL,
	}, []string{"writingbench", "eqbench-creative-v3", WritingPrimaryID})
	if err != nil {
		t.Fatal(err)
	}
	if writingRequests != 1 || eqBenchRequests != 1 || lechRequests != 1 || len(statuses) != 3 {
		t.Fatalf("unexpected source-scoped refresh: writing=%d eq=%d lech=%d statuses=%#v", writingRequests, eqBenchRequests, lechRequests, statuses)
	}
	for _, name := range []string{"writingbench", "eqbench-creative-v3", WritingPrimaryID} {
		snapshot, err := readSnapshot(dir + "/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if !snapshot.FetchedAt.Equal(now) || len(snapshot.Models) == 0 {
			t.Fatalf("unexpected %s snapshot: %#v", name, snapshot)
		}
		for _, model := range snapshot.Models {
			for benchmark, result := range model.Benchmarks {
				if result.SourceID != name || result.SourceClass == "" || result.EvidenceGrade == "" || result.Method == "" || result.ContentSHA == "" {
					t.Fatalf("invalid %s/%s provenance: %#v", name, benchmark, result)
				}
			}
		}
	}
}

func TestRefreshEvaluationSourcesRejectsUnknownBeforeFetch(t *testing.T) {
	dir := t.TempDir()
	_, _, err := RefreshEvaluationSources(context.Background(), Options{CacheDir: dir}, []string{"writingbench", "artificial-analysis"})
	if err == nil || err.Error() != "unsupported evaluation source \"artificial-analysis\" (supported: arena-creative-writing, eqbench-creative-v3, lechmazur-writing, writingbench)" {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := readSnapshot(dir + "/writingbench.json"); !os.IsNotExist(err) {
		t.Fatalf("rejected refresh wrote a cache: %v", err)
	}
}

func TestPreserveEvaluationSourceCachesPreservesPredecessors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "writingbench.json"), []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	backups, err := preserveEvaluationSourceCaches(dir, []string{"writingbench", "eqbench-creative-v3"}, time.Unix(1, 0).UTC())
	if err != nil || len(backups) != 1 {
		t.Fatalf("unexpected backups: %#v %v", backups, err)
	}
	data, err := os.ReadFile(backups[0])
	if err != nil || string(data) != "previous" {
		t.Fatalf("predecessor was not preserved: %q %v", data, err)
	}
}

func TestRefreshLLMStatsStatsV1SourceReplacesCacheAfterValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing Bearer token: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			if r.URL.Query().Get("cursor") == "" {
				_, _ = w.Write([]byte(`{"models":[{"id":"m1","name":"M1","organization":{"id":"lab","name":"Lab"},"license":{"id":"mit","name":"MIT"},"open_weight":true,"model_type":"llm","providers":[]},{"id":"m2","name":"M2","organization":{"id":"lab","name":"Lab"},"license":{"id":"mit","name":"MIT"},"open_weight":true,"model_type":"llm","providers":[]},{"id":"image","name":"Image","organization":{"id":"lab","name":"Lab"},"license":{"id":"mit","name":"MIT"},"open_weight":false,"model_type":"image","providers":[]}],"next_cursor":"page-2","total":6}`))
				return
			}
			_, _ = w.Write([]byte(`{"models":[{"id":"m3","name":"M3","organization":{"id":"lab","name":"Lab"},"license":{"id":"mit","name":"MIT"},"open_weight":true,"model_type":"llm","providers":[]},{"id":"m4","name":"M4","organization":{"id":"lab","name":"Lab"},"license":{"id":"mit","name":"MIT"},"open_weight":true,"model_type":"llm","providers":[]},{"id":"m5","name":"M5","organization":{"id":"lab","name":"Lab"},"license":{"id":"mit","name":"MIT"},"open_weight":true,"model_type":"llm","providers":[]}],"next_cursor":"","total":6}`))
		case "/v1/benchmarks":
			_, _ = w.Write([]byte(`{"benchmarks":[{"id":"gpqa","name":"GPQA","model_count":5}]}`))
		case "/v1/scores":
			rows := make([]string, 0, 5)
			for index := 1; index <= 5; index++ {
				rows = append(rows, statsV1ScoreRow(fmt.Sprintf("m%d", index), float64(index)/10, true))
			}
			_, _ = fmt.Fprintf(w, `{"scores":[%s],"next_cursor":"","total":5}`, strings.Join(rows, ","))
		default:
			t.Errorf("unexpected Stats v1 request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	previous := filepath.Join(dir, "llm-stats.json")
	if err := writeSnapshot(previous, sourceSnapshot{FetchedAt: time.Unix(1, 0).UTC(), Models: []Model{{Key: "previous"}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(10, 0).UTC()
	status, backup, err := RefreshLLMStatsStatsV1Source(context.Background(), Options{
		CacheDir: dir, LLMStatsAPIKey: "secret", Client: &http.Client{},
		LLMStatsStatsV1ModelsURL:     server.URL + "/v1/models",
		LLMStatsStatsV1BenchmarksURL: server.URL + "/v1/benchmarks",
		LLMStatsStatsV1ScoresURL:     server.URL + "/v1/scores",
		Now:                          func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.Models != 5 || status.Observations != 5 || status.Cache != "fetched" || status.RegistryVersion != SourceRegistryVersion || status.ContentSHA == "" {
		t.Fatalf("unexpected Stats v1 status: %#v", status)
	}
	if backup == "" {
		t.Fatal("predecessor cache was not preserved")
	}
	if data, err := os.ReadFile(backup); err != nil || !strings.Contains(string(data), `"key": "previous"`) {
		t.Fatalf("invalid predecessor backup: %s %v", data, err)
	}
	snapshot, err := readSnapshot(previous)
	if err != nil || len(snapshot.Models) != 5 || snapshot.Observations != 5 || !snapshot.FetchedAt.Equal(now) || snapshot.RegistryVersion != SourceRegistryVersion {
		t.Fatalf("unexpected replacement snapshot: %#v %v", snapshot, err)
	}
	for _, model := range snapshot.Models {
		if model.Benchmarks["gpqa"].SourceID != llmStatsStatsV1SourceID {
			t.Fatalf("Stats v1 observations were not attached: %#v", model)
		}
	}
	for pattern, want := range map[string]int{
		filepath.Join(dir, "sealed", "llm-stats-stats-v1", "models-*.json"):        2,
		filepath.Join(dir, "sealed", "llm-stats-stats-v1", "catalog-*.json"):       1,
		filepath.Join(dir, "sealed", "llm-stats-stats-v1", "observations-*.jsonl"): 1,
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) != want {
			t.Fatalf("unexpected sealed Stats v1 artifact %s: %#v %v", pattern, matches, err)
		}
	}
}
