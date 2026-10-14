package evals

import "sort"

func normalizedCompleteCohorts(contributions []NormalizedScoreContributionRecord, frozenCohorts map[string]NormalizedFrozenCohortRecord) []NormalizedFrozenCohortRecord {
	for _, contribution := range contributions {
		key, revision := normalizedCohortKey(contribution.Benchmark, contribution.SourceRevision)
		cohort, ok := frozenCohorts[key]
		if !ok {
			cohort = NormalizedFrozenCohortRecord{Benchmark: contribution.Benchmark, Revision: revision}
		}
		cohort.SourceIDs = normalizedAppendUnique(cohort.SourceIDs, contribution.SourceID)
		if _, targeted := frozenLLMStatsCohortPopulations[contribution.Benchmark]; targeted {
			cohort.SourceIDs = normalizedAppendUnique(cohort.SourceIDs, "llm-stats-frozen-cohorts")
		}
		frozenCohorts[key] = cohort
	}
	for key, cohort := range frozenCohorts {
		if len(cohort.Values) == 0 {
			cohort.Values = normalizedFrozenCohortValues(cohort.Benchmark, cohort.Revision)
		}
		cohort.Population = len(cohort.Values)
		sort.Strings(cohort.SourceIDs)
		frozenCohorts[key] = cohort
	}
	rows := make([]NormalizedFrozenCohortRecord, 0, len(frozenCohorts))
	for _, cohort := range frozenCohorts {
		if cohort.Population > 0 {
			rows = append(rows, cohort)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Benchmark != rows[j].Benchmark {
			return rows[i].Benchmark < rows[j].Benchmark
		}
		return rows[i].Revision < rows[j].Revision
	})

	return rows
}
