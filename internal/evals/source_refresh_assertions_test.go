package evals

import (
	"testing"
	"time"
)

func assertRefreshedWritingEvidenceRows(t *testing.T, dir string, now time.Time) {
	t.Helper()
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
