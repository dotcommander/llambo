package evals

import (
	"context"
	"strings"
)

type llmStatsDiscoveryBenchmarkResult struct {
	observations []Observation
	normalized   []byte
	method       string
}

func acquireLLMStatsDiscoveryBenchmarks(ctx context.Context, opts Options, method string) (llmStatsDiscoveryBenchmarkResult, error) {
	var observations []Observation
	var err error
	if strings.TrimSpace(opts.LLMStatsAPIKey) != "" {
		observations, err = fetchLLMStatsStatsV1BenchmarkLeads(ctx, opts)
		method = "LLM Stats public discovery feeds plus Stats v1 benchmark scores; published rows use graded verification evidence"
	} else {
		observations, err = fetchLLMStatsBenchmarkLeads(ctx, opts)
	}
	if err != nil {
		return llmStatsDiscoveryBenchmarkResult{method: method}, err
	}
	normalized, err := EncodeObservationsJSONL(observations)
	return llmStatsDiscoveryBenchmarkResult{observations: observations, normalized: normalized, method: method}, err
}
