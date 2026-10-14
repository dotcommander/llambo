package evals

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func statsV1RefreshFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
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
	t.Cleanup(server.Close)
	return server
}
