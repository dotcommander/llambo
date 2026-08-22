package evals

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestFetchWritingBenchRecordsContentProvenance(t *testing.T) {
	body := syntheticWritingBenchXLSX(t)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Etag": {`"wb-v1"`}, "X-Repo-Commit": {"abc123"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	})}
	snapshot, err := fetchWritingBench(context.Background(), Options{Client: client, WritingBenchURL: "https://example.test/score.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Models) != 1 {
		t.Fatalf("models = %#v", snapshot.Models)
	}
	want := sha256.Sum256(body)
	result := snapshot.Models[0].Benchmarks["writingbench"]
	if result.ContentSHA != fmtSHA(want) || result.URL != "https://example.test/score.xlsx" || result.Version != "wb-v1" || result.CommitSHA != "abc123" || result.SourceID != "writingbench" || result.SourceClass != string(SourceOwnerResult) || result.EvidenceGrade != "owner" || result.SourceRevision != "abc123" || result.Method == "" {
		t.Fatalf("provenance = %#v", result)
	}
}

func TestWritingBenchCacheIsUsedWithoutRefresh(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(100, 0).UTC()
	cache := sourceSnapshot{FetchedAt: now, Models: []Model{{Name: "Cached", Organization: "Example", Benchmarks: map[string]BenchmarkResult{"writingbench": {}}}}}
	if err := writeSnapshot(dir+"/writingbench.json", cache); err != nil {
		t.Fatal(err)
	}
	called := false
	snapshot, status, err := loadSource(context.Background(), Options{CacheDir: dir, TTL: time.Hour, Now: func() time.Time { return now }, WritingBenchURL: "https://example.test/score.xlsx"}, "writingbench", func(context.Context) (sourceSnapshot, error) { called = true; return sourceSnapshot{}, nil })
	if err != nil || called || status.Cache != "fresh" || len(snapshot.Models) != 1 {
		t.Fatalf("snapshot=%#v status=%#v err=%v called=%t", snapshot, status, err, called)
	}
}

func TestMergeWritingBenchModelsRejectsAmbiguity(t *testing.T) {
	score := 8.7
	base := []Model{{Key: "a", Name: "Model X", Organization: "Example Labs"}, {Key: "b", Name: "Model X", Organization: "Example Labs"}}
	bench := []Model{{Name: "Model X", Organization: "Example Labs", Benchmarks: map[string]BenchmarkResult{"writingbench": {Score: &score}}}}
	if got := mergeWritingBenchModels(base, bench); got[0].Benchmarks != nil || got[1].Benchmarks != nil {
		t.Fatalf("ambiguous identity was merged: %#v", got)
	}
	base = []Model{{Key: "a", Name: "Model X", Organization: "Example Labs"}}
	if got := mergeWritingBenchModels(base, bench); got[0].Benchmarks["writingbench"].Score == nil {
		t.Fatalf("unique normalized identity was not merged: %#v", got)
	}
}

func fmtSHA(sum [32]byte) string { return fmt.Sprintf("%x", sum) }
