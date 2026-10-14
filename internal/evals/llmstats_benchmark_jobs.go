package evals

import (
	"context"
	"sync"
)

type llmStatsBenchmarkJobResult struct {
	benchmark    string
	observations []Observation
	err          error
}

// startLLMStatsBenchmarkJobs owns the bounded source worker lifecycle. Workers
// exit when the producer closes jobs; the producer exits after its bounded input
// or context cancellation; the coordinator closes results after all workers exit.
func startLLMStatsBenchmarkJobs[T any](ctx context.Context, benchmarks []T, fetch func(T) llmStatsBenchmarkJobResult) <-chan llmStatsBenchmarkJobResult {
	jobs := make(chan T)
	results := make(chan llmStatsBenchmarkJobResult, len(benchmarks))
	var wg sync.WaitGroup
	for range min(8, max(1, len(benchmarks))) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for benchmark := range jobs {
				results <- fetch(benchmark)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, benchmark := range benchmarks {
			select {
			case jobs <- benchmark:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	return results
}
