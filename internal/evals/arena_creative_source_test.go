package evals

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const arenaCreativeFixture = `{"overall":{"other":{"rating":1000,"rating_q025":990,"rating_q975":1010}},"creative_writing":{"model-a":{"rating":1500,"rating_q025":1490,"rating_q975":1510},"model-b":{"rating":1400,"rating_q025":1380,"rating_q975":1420}}}`

func TestFetchArenaCreativeNormalizesOnlyCreativeWriting(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Etag": {`"arena-v1"`}}, Body: io.NopCloser(strings.NewReader(arenaCreativeFixture)), Request: request}, nil
	})}
	snapshot, err := fetchArenaCreative(context.Background(), Options{Client: client, Now: func() time.Time { return time.Unix(1, 0).UTC() }, ArenaCreativeURL: "https://example.test/leaderboard.json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Models) != 2 || snapshot.Version != "arena-v1" || snapshot.Observations != 2 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	result := snapshot.Models[0].Benchmarks["lmsys-writing"]
	if result.Score == nil || result.SourceID != ArenaCreativeSourceID || result.Details["rating_q025"] == 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestNormalizeHTTPETag(t *testing.T) {
	for input, want := range map[string]string{`"strong"`: "strong", `W/"weak"`: "weak", " plain ": "plain"} {
		if got := normalizeHTTPETag(input); got != want {
			t.Errorf("normalizeHTTPETag(%q) = %q, want %q", input, got, want)
		}
	}
}
