package evals

import (
	"fmt"
)

func validateOfficialCardModel(spec officialCardSpec, want map[string]map[string]float64, model Model) error {
	expectedScores, ok := want[model.Key]
	if !ok {
		return fmt.Errorf("canonical card model %q is not pinned", model.Key)
	}
	if model.IdentityMatch != IdentityMatchExact {
		return fmt.Errorf("canonical card model %q identity is %q, want exact", model.Key, model.IdentityMatch)
	}
	if len(model.Benchmarks) != len(expectedScores) {
		return fmt.Errorf("model %q benchmark count %d does not match pinned row count %d", model.Key, len(model.Benchmarks), len(expectedScores))
	}
	for benchmark, expected := range expectedScores {
		result, present := model.Benchmarks[benchmark]
		if !present {
			return fmt.Errorf("model %q missing pinned benchmark %q", model.Key, benchmark)
		}
		if result.Score == nil || *result.Score != expected {
			return fmt.Errorf("model %q benchmark %q score does not match pinned source row", model.Key, benchmark)
		}
		if !officialCardBenchmarkProvenanceMatches(spec, benchmark, result) {
			return fmt.Errorf("model %q benchmark %q provenance does not match pinned source", model.Key, benchmark)
		}
	}
	return nil
}

func officialCardBenchmarkProvenanceMatches(spec officialCardSpec, benchmark string, result BenchmarkResult) bool {
	identity := result.Identity == IdentityMatchExact && result.Version == spec.revision && result.URL == pinnedOfficialCardURL(spec) && result.ContentSHA == spec.contentSHA
	method := result.Method == modelCardSourceType && result.EvidenceGrade == modelCardEvidence && result.Unit == modelCardUnit && result.Direction == modelCardDirection
	row := result.Cohort == "official-comparison-table" && result.Locator == "published benchmark table: "+benchmark && result.SourceID == spec.sourceID && result.SourceClass == string(SourceFirstPartyResult) && result.SourceRevision == spec.revision
	return identity && method && row
}
