package evals

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func statsV1BearerPaginationFixtureClient(t *testing.T) *http.Client {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing Bearer token: %q", request.Header.Get("Authorization"))
		}
		body := ""
		switch request.URL.Path {
		case "/stats/v1/benchmarks":
			body = `{"benchmarks":[{"id":"unreviewed","name":"Unreviewed","model_count":99},{"id":"gpqa","name":"GPQA","model_count":5}]}`
		case "/stats/v1/scores":
			if request.URL.Query().Get("benchmark") != "gpqa" || request.URL.Query().Get("limit") != "500" {
				t.Fatalf("unexpected scores query: %s", request.URL.RawQuery)
			}
			switch cursor := request.URL.Query().Get("cursor"); cursor {
			case "":
				body = fmt.Sprintf(`{"scores":[%s,%s,%s],"next_cursor":"cursor-2","total":6}`, statsV1ScoreRow("model-a", .946, true), statsV1ScoreRow("model-a", .946, true), statsV1ScoreRow("model-b", 0, false))
			case "cursor-2":
				body = fmt.Sprintf(`{"scores":[%s,%s,%s,%s],"next_cursor":"","total":6}`, statsV1ScoreRow("model-c", .25, true), statsV1ScoreRow("model-d", .75, true), statsV1ScoreRow("model-e", .6, false), statsV1ScoreRow("model-a", .946, true))
			default:
				t.Fatalf("unexpected cursor %q", cursor)
			}
		default:
			return nil, fmt.Errorf("unexpected request %s", request.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return client
}
