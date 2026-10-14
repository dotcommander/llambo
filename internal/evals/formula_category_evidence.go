package evals

import "math"

type resultEvidenceInput struct {
	benchmark, family string
	weight            float64
	result            BenchmarkResult
	percentile        float64
	population        int
}

func selectFamilyEvidence(model Model, family benchmarkFamily, context scoringContext) (*benchmarkEvidence, []string) {
	conflicts := make([]string, 0)
	var selected *benchmarkEvidence
	for _, benchmark := range family.benchmarks {
		result, ok := model.Benchmarks[benchmark]
		if ok && result.Quarantined {
			conflicts = append(conflicts, benchmark+": "+result.Conflict)
			continue
		}
		if !ok || !eligibleCanonicalResult(result) {
			continue
		}
		cohort := context.resolvedFrozenBenchmarkCohort(benchmark, result)
		if len(cohort) < 5 {
			continue
		}
		higher := result.Direction != lower1
		percentile := empiricalPercentile(cohort, *result.Score, higher)
		trust := evidenceMultiplier(result.EvidenceGrade) * identityMultiplier(result.Identity)
		evidence := evidenceForResult(resultEvidenceInput{benchmark: benchmark, family: family.name, weight: 1, result: result, percentile: percentile, population: len(cohort)})
		evidence.EvidenceMultiplier, evidence.IdentityMultiplier, evidence.EffectiveWeight, evidence.Stale = evidenceMultiplier(result.EvidenceGrade), identityMultiplier(result.Identity), trust, result.Stale
		// A family contributes once. Its first reviewed benchmark in registry
		// order wins, so duplicates never amplify a model's score.
		selected = &evidence
		break
	}
	return selected, conflicts
}

func legacyCategoryChecks(model Model, legacy categorySpec, selected benchmarkSpec, reference FormulaReference, percentile float64) ([]benchmarkEvidence, []float64) {
	checks := make([]benchmarkEvidence, 0, len(legacy.checks))
	deltas := make([]float64, 0, len(legacy.checks))
	for _, check := range legacy.checks {
		value, ok := check.value(model)
		values := reference.Metrics[check.reference]
		if check.benchmark == selected.benchmark || !ok || value == nil || !finite(*value) || len(values) == 0 {
			continue
		}
		checkPercentile := empiricalPercentile(values, *value, true)
		checks = append(checks, evidenceFor(model, check, value, checkPercentile, len(values)))
		deltas = append(deltas, math.Abs(checkPercentile-percentile))
	}
	return checks, deltas
}
