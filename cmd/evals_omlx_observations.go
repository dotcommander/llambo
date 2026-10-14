package cmd

import (
	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/evals"
)

func omlxOperationalObservations(entry *catalog.ModelEntry, costMap map[string]costs.ModelCost, key string) []evals.OMLXOperationalObservation {
	if entry == nil {
		return nil
	}
	var observations []evals.OMLXOperationalObservation
	for source, benchmark := range entry.Benchmarks {
		if benchmark.SpeedTokensPerSecond > 0 {
			observations = append(observations, evals.OMLXOperationalObservation{Metric: "speed", Value: benchmark.SpeedTokensPerSecond, Source: source, MeasuredAt: benchmark.UpdatedAt, Benchmark: true})
		}
		if benchmark.LatencyMS > 0 {
			observations = append(observations, evals.OMLXOperationalObservation{Metric: "latency", Value: float64(benchmark.LatencyMS), Source: source, MeasuredAt: benchmark.UpdatedAt, Benchmark: true})
		}
	}
	if entry.LastPing.Success {
		if entry.LastPing.SpeedTokensPerSecond > 0 {
			observations = append(observations, evals.OMLXOperationalObservation{Metric: "speed", Value: entry.LastPing.SpeedTokensPerSecond, Source: ping1, MeasuredAt: entry.LastPing.CheckedAt, Successful: true})
		}
		if entry.LastPing.LatencyMS > 0 {
			observations = append(observations, evals.OMLXOperationalObservation{Metric: "latency", Value: float64(entry.LastPing.LatencyMS), Source: ping1, MeasuredAt: entry.LastPing.CheckedAt, Successful: true})
		}
	}
	status, _, output := catalog.CostForEntry(costMap, omlx1, key, entry)
	if status != catalog.CostUnknown {
		observations = append(observations, evals.OMLXOperationalObservation{Metric: "output-price", Value: output, Source: "catalog-pricing", TimestampUnknown: true, Benchmark: true})
	}
	return observations
}
