package evals

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFetchLechMazurWritingNormalizesLeaderboard(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Etag": {`"revision-1"`}},
			Body:       io.NopCloser(strings.NewReader(writingLeaderboardFixture)),
			Request:    request,
		}, nil
	})}
	snapshot, err := fetchLechMazurWriting(context.Background(), Options{
		Client: client, Now: func() time.Time { return now }, LechMazurWritingURL: "https://example.test/README.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Models) != 2 || snapshot.Version != "revision-1" || snapshot.ContentSHA == "" || snapshot.Observations != 2 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	result := snapshot.Models[0].Benchmarks["fiction-live"]
	if result.Score == nil || *result.Score != 3.3 || result.SourceID != WritingPrimaryID || result.SourceClass != string(SourceOwnerResult) || result.Details["win_chance_percent"] != 91 {
		t.Fatalf("unexpected normalized result: %#v", result)
	}
}
