package evals

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func assertStatsV1RefreshedCacheReuse(t *testing.T, dir string, now time.Time) {
	t.Helper()
	// Cache reuse must read the same filename as the publisher, without credentials.
	statuses, backups, err := RefreshEvaluationSourcesWithOptions(context.Background(), Options{
		CacheDir: dir, Now: func() time.Time { return now.Add(time.Hour) },
	}, []string{"llm-stats-stats-v1"}, RefreshSourceOptions{TTL: 4 * time.Hour})
	if err != nil || len(statuses) != 1 || statuses[0].Cache != "cached" || statuses[0].Models != 5 || len(backups) != 0 {
		t.Fatalf("Stats v1 cache reuse failed: %#v %v %v", statuses, backups, err)
	}
}

func assertStatsV1ReplacementSnapshot(t *testing.T, previous string, now time.Time) {
	t.Helper()
	snapshot, err := readSnapshot(previous)
	if err != nil || len(snapshot.Models) != 5 || snapshot.Observations != 5 || !snapshot.FetchedAt.Equal(now) || snapshot.RegistryVersion != SourceRegistryVersion {
		t.Fatalf("unexpected replacement snapshot: %#v %v", snapshot, err)
	}
	for _, model := range snapshot.Models {
		if model.Benchmarks["gpqa"].SourceID != llmStatsStatsV1SourceID {
			t.Fatalf("Stats v1 observations were not attached: %#v", model)
		}
	}
}

func assertStatsV1ReplacementArtifacts(t *testing.T, dir string) {
	t.Helper()
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
