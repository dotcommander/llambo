package evals

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

func llmStatsOffsetFixtureClient(mu *sync.Mutex, offsets *[]int) *http.Client {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch request.URL.Path {
		case "/models":
			rows := make([]string, 21)
			for i := range rows {
				rows[i] = fmt.Sprintf(`{"model_id":"m%d","name":"M%d","model_type":"llm","organization":"Lab","is_open":true}`, i, i)
			}
			body = "[" + strings.Join(rows, ",") + "]"
		case "/full":
			body = "[]"
		case "/indexes":
			body = "{}"
		case "/benchmarks":
			body = `[{"benchmark_id":"gpqa","name":"GPQA","max_score":1,"model_count":21}]`
		case "/benchmarks/gpqa":
			offset, _ := strconv.Atoi(request.URL.Query().Get("offset"))
			mu.Lock()
			*offsets = append(*offsets, offset)
			mu.Unlock()
			count := 20
			if offset == 20 {
				count = 1
			}
			entries := make([]string, count)
			for i := range entries {
				id := offset + i
				entries[i] = fmt.Sprintf(`{"model_id":"m%d","organization_id":"lab","benchmark_score":%d,"verified":false,"self_reported":true}`, id, id)
			}
			body = fmt.Sprintf(`{"benchmark_id":"gpqa","max_score":1,"total_models":21,"entries":[%s]}`, strings.Join(entries, ","))
		default:
			return nil, fmt.Errorf("unexpected request %s", request.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return client
}
