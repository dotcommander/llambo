package evals

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestParseEQBenchCreativeJS(t *testing.T) {
	rows, err := ParseEQBenchCreativeJS(syntheticEQBenchCreativeJS())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Model != "Model X" || rows[0].Result.Score == nil || *rows[0].Result.Score != 1234 || rows[0].Result.Details["creative_writing_score"] != 8.5 || rows[0].Result.Details["rubric_score"] != 7.2 {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestParseEQBenchCreativeJSRejectsMalformedSource(t *testing.T) {
	for _, source := range [][]byte{[]byte("const x = `model_name,elo_score\nModel X,not-a-number`"), []byte("const x = `model_name,elo_score\nModel X,NaN`"), []byte("const x = `other,value\nmodel,1`")} {
		if _, err := ParseEQBenchCreativeJS(source); err == nil {
			t.Fatalf("malformed source accepted: %q", source)
		}
	}
}

func TestFetchEQBenchCreativeRecordsPinnedProvenance(t *testing.T) {
	body := syntheticEQBenchCreativeJS()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})}
	now := time.Unix(123, 0).UTC()
	snapshot, err := fetchEQBenchCreative(context.Background(), Options{Client: client, EQBenchCreativeURL: "https://example.test/creative_writing.js", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	result := snapshot.Models[0].Benchmarks["eqbench-creative-v3"]
	want := sha256.Sum256(body)
	if result.CommitSHA != EQBenchCreativeCommit || result.Version != EQBenchCreativeCommit || result.ContentSHA != fmtSHA(want) || result.SourceID != "eqbench-creative-v3" || result.SourceClass != string(SourceOwnerResult) || result.EvidenceGrade != "owner" || result.SourceRevision != EQBenchCreativeCommit || !result.FetchedAt.Equal(now) || result.Method == "" {
		t.Fatalf("provenance = %#v", result)
	}
}

func TestEQBenchCreativeCacheAndIdentityMerge(t *testing.T) {
	dir, now := t.TempDir(), time.Unix(123, 0).UTC()
	cache := sourceSnapshot{FetchedAt: now, Models: []Model{{Name: "Model X", Benchmarks: map[string]BenchmarkResult{"eqbench-creative-v3": {}}}}}
	if err := writeSnapshot(dir+"/eqbench-creative-v3.json", cache); err != nil {
		t.Fatal(err)
	}
	called := false
	if _, status, err := loadSource(context.Background(), Options{CacheDir: dir, TTL: time.Hour, Now: func() time.Time { return now }, EQBenchCreativeURL: "https://example.test/creative_writing.js"}, "eqbench-creative-v3", func(context.Context) (sourceSnapshot, error) { called = true; return sourceSnapshot{}, nil }); err != nil || called || status.Cache != "fresh" {
		t.Fatalf("status=%#v err=%v called=%t", status, err, called)
	}
	score := 1234.0
	bench := []Model{{Name: "Model X", Benchmarks: map[string]BenchmarkResult{"eqbench-creative-v3": {Score: &score}}}}
	if got := mergeEQBenchCreativeModels([]Model{{Name: "Model X", Organization: "A"}, {Name: "Model X", Organization: "B"}}, bench); got[0].Benchmarks != nil || got[1].Benchmarks != nil {
		t.Fatalf("ambiguous name merge: %#v", got)
	}
	if got := mergeEQBenchCreativeModels([]Model{{Name: "Model X", Organization: "A"}}, bench); got[0].Benchmarks["eqbench-creative-v3"].Score == nil {
		t.Fatalf("unique name merge failed: %#v", got)
	}
}

func syntheticEQBenchCreativeJS() []byte {
	return []byte("const creativeWriting = `model_name,elo_score,creative_writing_score,rubric_score\nModel X,1234,8.5,7.2\n`; export default creativeWriting;")
}
