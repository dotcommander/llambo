package evals

import (
	"fmt"
)

func validateFrozenLLMStatsCohortRows(benchmark string, cohort frozenLLMStatsCohort, rows []Observation) error {
	if len(rows) != cohort.RowCount {
		return fmt.Errorf("frozen LLM Stats cohort %s has %d rows, want %d", benchmark, len(rows), cohort.RowCount)
	}
	scores := make([]float64, 0, len(rows))
	digests := make(map[string]struct{}, len(cohort.Digests))
	modelIDs := make(map[string]struct{}, len(rows))
	for _, observation := range rows {
		if !frozenLLMStatsRowCompatible(observation, cohort) {
			return fmt.Errorf("frozen LLM Stats cohort %s has an incompatible row", benchmark)
		}
		if _, duplicate := modelIDs[observation.ModelID]; duplicate {
			return fmt.Errorf("frozen LLM Stats cohort %s has duplicate model ID %s", benchmark, observation.ModelID)
		}
		modelIDs[observation.ModelID] = struct{}{}
		digests[observation.SourceRevision] = struct{}{}
		scores = append(scores, observation.RawScore)
	}
	return validateFrozenLLMStatsCohortPopulation(benchmark, cohort, digests, scores)
}

func frozenLLMStatsRowCompatible(observation Observation, cohort frozenLLMStatsCohort) bool {
	identity := observation.SourceID == cohort.SourceID && string(observation.SourceClass) == cohort.SourceClass && observation.BenchmarkVersion == cohort.BenchmarkVersion && observation.Cohort == cohort.Cohort
	provenance := observation.Methodology == cohort.Methodology && observation.Direction == cohort.Direction && observation.SourceRevision != "" && observation.SourceRevision == observation.SourceSHA256
	return identity && provenance
}
