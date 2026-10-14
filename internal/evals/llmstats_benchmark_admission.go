package evals

import (
	"fmt"
	"strings"
)

type llmStatsBenchmarkPageEvidence struct {
	benchmark string
	offset    int
	digest    string
	endpoint  string
}

func admitLLMStatsBenchmarkPage(opts Options, page llmStatsBenchmarkPage, evidence llmStatsBenchmarkPageEvidence, seen map[string]struct{}) ([]Observation, error) {
	observations := make([]Observation, 0, len(page.Entries))
	for index, entry := range page.Entries {
		if strings.TrimSpace(entry.ModelID) == "" || entry.Score == nil {
			return nil, fmt.Errorf("benchmark %s offset %d row %d is malformed", evidence.benchmark, evidence.offset, index+1)
		}
		if _, ok := seen[entry.ModelID]; ok {
			return nil, fmt.Errorf("benchmark %s has duplicate model %q", evidence.benchmark, entry.ModelID)
		}
		seen[entry.ModelID] = struct{}{}
		grade := aggregatorUnspecified
		if entry.Verified {
			grade = aggregatorVerified
		} else if entry.SelfReported {
			grade = aggregatorSelfReported
		}
		observations = append(observations, Observation{SourceID: llmStatsBenchmarkResults, SourceRevision: evidence.digest, SourceSHA256: evidence.digest, Benchmark: evidence.benchmark, BenchmarkVersion: publicLeaderboard, Cohort: "public", ModelID: entry.ModelID, OrganizationID: entry.OrganizationID, RawScore: *entry.Score, Unit: "source_native", Direction: higher1, Methodology: "LLM Stats public leaderboard aggregation; verification status supplied per row", EvidenceGrade: grade, FetchedAt: opts.Now().UTC(), Locator: fmt.Sprintf("%s#entries[%d]", evidence.endpoint, index), SourceClass: SourceAggregatorResult})
	}
	return observations, nil
}
