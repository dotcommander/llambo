package evals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWritingEvidenceSourceDescriptorsOwnDefaultsAndMetadata(t *testing.T) {
	t.Parallel()
	opts := Options{
		WritingBenchURL:     "https://example.test/writingbench",
		EQBenchCreativeURL:  "https://example.test/eqbench",
		LechMazurWritingURL: "https://example.test/lechmazur",
		ArenaCreativeURL:    "https://example.test/arena",
	}
	wantIDs := []string{"writingbench", "eqbench-creative-v3", WritingPrimaryID, ArenaCreativeSourceID}
	wantNames := []string{"WritingBench", "EQ-Bench Creative v3", "Lech Mazur Creative Story-Writing", "Arena Creative Writing"}
	wantURLs := []string{opts.WritingBenchURL, opts.EQBenchCreativeURL, opts.LechMazurWritingURL, opts.ArenaCreativeURL}

	if got := defaultWritingEvidenceSourceNames(); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("default source IDs = %v, want %v", got, wantIDs)
	}
	for i, id := range wantIDs {
		source, ok := writingEvidenceSourceFor(id)
		if !ok {
			t.Fatalf("source %q missing from descriptor lookup", id)
		}
		if source.name != wantNames[i] || source.url(opts) != wantURLs[i] || source.fetcher == nil {
			t.Errorf("source %q metadata = name %q url %q fetcher_nil=%t", id, source.name, source.url(opts), source.fetcher == nil)
		}
	}
}

func TestRefreshEvaluationSourcesIsScopedAndValidatesProvenance(t *testing.T) {
	t.Parallel()
	var writingRequests, eqBenchRequests, lechRequests int
	writing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writingRequests++
		w.Header().Set("ETag", `"wb-etag"`)
		w.Header().Set("X-Repo-Commit", "wb-commit")
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		_, _ = w.Write(syntheticWritingBenchXLSX(t))
	}))
	t.Cleanup(writing.Close)
	eqBench := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eqBenchRequests++
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(syntheticEQBenchCreativeJS())
	}))
	t.Cleanup(eqBench.Close)
	lech := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lechRequests++
		w.Header().Set("ETag", `"lech-revision"`)
		_, _ = w.Write([]byte(writingLeaderboardFixture))
	}))
	t.Cleanup(lech.Close)

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
	assertRefreshedWritingEvidenceRows(t, dir, now)
}

func TestRefreshEvaluationSourcesRejectsUnknownBeforeFetch(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	server := statsV1RefreshFixtureServer(t)

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
	assertStatsV1RefreshedCacheReuse(t, dir, now)
	if backup == "" {
		t.Fatal("predecessor cache was not preserved")
	}
	var data []byte
	if data, err = os.ReadFile(backup); err != nil || !strings.Contains(string(data), `"key": "previous"`) {
		t.Fatalf("invalid predecessor backup: %s %v", data, err)
	}
	assertStatsV1ReplacementSnapshot(t, previous, now)
	assertStatsV1ReplacementArtifacts(t, dir)
}

func TestRefreshEvaluationSources_CachingAndIdempotency(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed writingbench snapshot fetched 1 hour ago
	wbPath := filepath.Join(dir, "writingbench.json")
	seeded := sourceSnapshot{
		FetchedAt:    now.Add(-1 * time.Hour),
		Models:       []Model{{Key: "seeded-model"}},
		Observations: 1,
		Version:      "rev-1",
		ContentSHA:   "sha256-abc",
		Method:       "test-method",
	}
	if err := writeSnapshot(wbPath, seeded); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		CacheDir: dir,
		Now:      func() time.Time { return now },
	}

	// 1. Within 4-hour TTL: served from cache, no backups, no network
	statuses, backups, err := RefreshEvaluationSourcesWithOptions(context.Background(), opts, []string{"writingbench"}, RefreshSourceOptions{
		TTL: 4 * time.Hour,
	})
	if err != nil {
		t.Fatalf("RefreshEvaluationSourcesWithOptions failed: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Cache != "cached" {
		t.Fatalf("expected cached status, got: %#v", statuses)
	}
	if statuses[0].Models != 1 || statuses[0].Observations != 1 {
		t.Fatalf("unexpected model/obs counts: %#v", statuses[0])
	}
	if len(backups) != 0 {
		t.Fatalf("expected no backups for cached run, got: %v", backups)
	}

	// 2. Second call: fully idempotent
	statuses2, backups2, err := RefreshEvaluationSourcesWithOptions(context.Background(), opts, []string{"writingbench"}, RefreshSourceOptions{
		TTL: 4 * time.Hour,
	})
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if len(statuses2) != 1 || statuses2[0].Cache != "cached" {
		t.Fatalf("second call must remain cached: %#v", statuses2)
	}
	if len(backups2) != 0 {
		t.Fatalf("second call must have no backups: %v", backups2)
	}
}

func TestSourceRefreshRejectsInvalidCache(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, tc := range []struct {
		name    string
		fetched time.Time
		sha     string
	}{
		{"future", now.Add(time.Hour), "sha"},
		{"incomplete", now.Add(-time.Hour), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := writeSnapshot(filepath.Join(dir, "writingbench.json"), sourceSnapshot{FetchedAt: tc.fetched, ContentSHA: tc.sha, Method: "test", Models: []Model{{Key: "model"}}}); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "expected fetch", http.StatusBadGateway) }))
			t.Cleanup(server.Close)
			statuses, _, err := RefreshEvaluationSourcesWithOptions(context.Background(), Options{CacheDir: dir, Now: func() time.Time { return now }, WritingBenchURL: server.URL}, []string{"writingbench"}, RefreshSourceOptions{TTL: 4 * time.Hour})
			if err == nil {
				t.Fatalf("invalid cache accepted: %#v", statuses)
			}
		})
	}
}
