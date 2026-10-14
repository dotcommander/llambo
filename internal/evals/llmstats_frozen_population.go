package evals

import (
	"fmt"
	"sort"
)

func validateFrozenLLMStatsCohortPopulation(benchmark string, cohort frozenLLMStatsCohort, digests map[string]struct{}, scores []float64) error {
	if len(digests) != len(cohort.Digests) {
		return fmt.Errorf("frozen LLM Stats cohort %s has incompatible digest set", benchmark)
	}
	for _, digest := range cohort.Digests {
		if _, ok := digests[digest]; !ok {
			return fmt.Errorf("frozen LLM Stats cohort %s is missing digest %s", benchmark, digest)
		}
	}
	sort.Float64s(scores)
	for index, score := range cohort.Scores {
		if scores[index] != score {
			return fmt.Errorf("frozen LLM Stats cohort %s has incompatible score population", benchmark)
		}
	}
	return nil
}
