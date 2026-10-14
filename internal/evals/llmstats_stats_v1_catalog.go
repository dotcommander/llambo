package evals

import (
	"fmt"
)

func eligibleLLMStatsStatsV1Benchmarks(catalog []llmStatsStatsV1Benchmark) ([]llmStatsStatsV1Benchmark, []map[string]string, error) {
	reviewed := reviewedBenchmarkSet()
	eligible := make([]llmStatsStatsV1Benchmark, 0, len(catalog))
	blocked := make([]map[string]string, 0)
	seen := make(map[string]struct{}, len(catalog))
	for _, benchmark := range catalog {
		if _, ok := seen[benchmark.ID]; ok {
			return nil, nil, fmt.Errorf("duplicate Stats v1 benchmark id %q", benchmark.ID)
		}
		seen[benchmark.ID] = struct{}{}
		if _, ok := reviewed[benchmark.ID]; !ok {
			continue
		}
		if benchmark.ModelCount < 5 {
			blocked = append(blocked, map[string]string{
				benchmark1: benchmark.ID,
				status1:    blocked1,
				reason1:    fmt.Sprintf("Stats v1 catalog has %d models, want at least 5", benchmark.ModelCount),
			})
			continue
		}
		eligible = append(eligible, benchmark)
	}
	for name := range reviewed {
		if _, ok := seen[name]; !ok {
			blocked = append(blocked, map[string]string{
				benchmark1: name,
				status1:    blocked1,
				reason1:    "Stats v1 catalog does not contain this reviewed benchmark",
			})
		}
	}
	return eligible, blocked, nil
}
