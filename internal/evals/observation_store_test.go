package evals

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRebuildObservationIndexAndDeterministicExport(t *testing.T) {
	observation := Observation{SourceID: "writingbench", SourceRevision: "v1", SourceSHA256: strings.Repeat("a", 64), Benchmark: "writingbench", BenchmarkVersion: "v1", Cohort: "official", ModelID: "model", RawScore: 1, Unit: "points", Direction: "higher", Methodology: "published", EvidenceGrade: "owner", FetchedAt: time.Unix(1, 0).UTC(), Locator: "row:1", SourceClass: SourceOwnerResult}
	path := filepath.Join(t.TempDir(), "observations.sqlite")
	if err := RebuildObservationIndex(context.Background(), path, []Observation{observation}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM observations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	first, err := EncodeObservationsJSONL([]Observation{observation})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeObservationsJSONL(strings.NewReader(string(first)), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeObservationsJSONL(decoded)
	if err != nil || string(first) != string(second) {
		t.Fatalf("normalized export changed: %v", err)
	}
}
