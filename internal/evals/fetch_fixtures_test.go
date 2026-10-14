package evals

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func fetchSourcesFixtureClient(t *testing.T, requests *atomic.Int64) *http.Client {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		var body string
		switch r.URL.Path {
		case "/models":
			body = `[{"model_id":"gpt-oss-20b-high","name":"GPT OSS 20B High","model_type":"llm","organization":"Lab","license":"apache_2_0","is_open":true},{"model_id":"closed-llm","name":"Closed LLM","model_type":"llm","organization":"Lab","is_open":false},{"model_id":"not-an-llm","name":"Image","model_type":"image","is_open":false}]`
		case "/full":
			body = `[{"model_id":"gpt-oss-20b-high","gpqa_score":0.8,"swe_bench_verified_score":0.7}]`
		case "/indexes":
			body = `{"coding":{"models":[{"model_id":"gpt-oss-20b-high","conservative":42,"mu":44,"sigma":1,"rank":2,"games_played":5}]}}`
		case "/aa":
			if r.Header.Get("x-api-key") != "secret" {
				t.Errorf("missing API key")
			}
			body = `{"intelligence_index_version":4.1,"pagination":{"page":1,"total_pages":1,"has_more":false},"data":[{"id":"aa-1","name":"GPT OSS 20B High","slug":"gpt-oss-20b","model_creator":{"name":"Lab"},"evaluations":{"artificial_analysis_intelligence_index":30,"artificial_analysis_coding_index":25},"pricing":{"price_1m_input_tokens":1},"performance":{"median_output_tokens_per_second":100}}]}`
		case "/score.xlsx":
			body = string(syntheticWritingBenchXLSX(t))
		case "/creative_writing.js":
			body = string(syntheticEQBenchCreativeJS())
		case "/lechmazur-writing.md":
			body = writingLeaderboardFixture
		case "/arena-creative.json":
			body = arenaCreativeFixture
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return client
}
