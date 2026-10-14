package evals

import (
	"errors"
	"fmt"
)

func validateFrozenLLMStatsStatsV1Cohorts(cohorts frozenLLMStatsCohorts) error {
	if cohorts.SchemaVersion != "llm-stats-stats-v1-frozen-cohorts-v1" || cohorts.SealedArtifactSHA256 != llmStatsStatsV1FrozenCohortsArtifactDigest || len(cohorts.Benchmarks) == 0 {
		return errors.New("frozen Stats v1 cohorts do not match the sealed artifact")
	}
	reviewed := reviewedBenchmarkSet()
	for benchmark, cohort := range cohorts.Benchmarks {
		if _, ok := reviewed[benchmark]; !ok || !validStatsV1CohortContract(cohort) {
			return fmt.Errorf("frozen Stats v1 cohort %s has an invalid contract", benchmark)
		}
		if err := validateFrozenCohortValues(benchmark, cohort, "frozen Stats v1"); err != nil {
			return err
		}
	}
	return nil
}

func validateFrozenLLMStatsCohorts(cohorts frozenLLMStatsCohorts) error {
	if cohorts.SchemaVersion != "llm-stats-frozen-cohorts-v1" || cohorts.SealedArtifactSHA256 != llmStatsFrozenCohortsArtifactDigest {
		return errors.New("frozen LLM Stats cohorts do not match the sealed artifact")
	}
	if len(cohorts.Benchmarks) != len(frozenLLMStatsCohortPopulations) {
		return fmt.Errorf("frozen LLM Stats cohorts have %d benchmarks, want %d", len(cohorts.Benchmarks), len(frozenLLMStatsCohortPopulations))
	}
	for benchmark, population := range frozenLLMStatsCohortPopulations {
		cohort, ok := cohorts.Benchmarks[benchmark]
		if !ok || !validLegacyCohortContract(cohort, population) {
			return fmt.Errorf("frozen LLM Stats cohort %s has an invalid contract", benchmark)
		}
		if err := validateFrozenCohortValues(benchmark, cohort, "frozen LLM Stats"); err != nil {
			return err
		}
	}
	for benchmark := range cohorts.Benchmarks {
		if _, ok := frozenLLMStatsCohortPopulations[benchmark]; !ok {
			return fmt.Errorf("frozen LLM Stats cohorts have unexpected benchmark %s", benchmark)
		}
	}
	return nil
}

func validStatsV1CohortContract(cohort frozenLLMStatsCohort) bool {
	return cohort.SourceID == llmStatsStatsV1SourceID &&
		cohort.SourceClass == string(SourceAggregatorResult) &&
		cohort.BenchmarkVersion == llmStatsStatsV1BenchmarkVersion &&
		cohort.Cohort == llmStatsStatsV1Cohort &&
		cohort.Methodology == llmStatsStatsV1Methodology &&
		cohort.Direction == higher1 &&
		cohort.MinPopulation == 5 &&
		cohort.RowCount >= 5 &&
		cohort.RowCount == len(cohort.Scores) &&
		len(cohort.Digests) != 0
}

func validLegacyCohortContract(cohort frozenLLMStatsCohort, population int) bool {
	return cohort.SourceID == llmStatsBenchmarkResults &&
		cohort.SourceClass == string(SourceAggregatorResult) &&
		cohort.BenchmarkVersion == publicLeaderboard &&
		cohort.Cohort == "public" &&
		cohort.Methodology == "LLM Stats public leaderboard aggregation; verification status supplied per row" &&
		cohort.Direction == higher1 &&
		cohort.MinPopulation == 5 &&
		cohort.RowCount == population &&
		len(cohort.Scores) == population &&
		len(cohort.Digests) != 0
}

func validateFrozenCohortValues(benchmark string, cohort frozenLLMStatsCohort, label string) error {
	for index, digest := range cohort.Digests {
		if !validSHA256(digest) || index > 0 && cohort.Digests[index-1] >= digest {
			return fmt.Errorf("%s cohort %s has invalid digests", label, benchmark)
		}
	}
	for index, score := range cohort.Scores {
		if !finite(score) || index > 0 && cohort.Scores[index-1] > score {
			return fmt.Errorf("%s cohort %s has invalid scores", label, benchmark)
		}
	}
	return nil
}
