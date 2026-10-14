package evals

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

func llmStatsStatsV1Observation(row llmStatsStatsV1ScoreRow, evidence statsV1ScorePageEvidence, index int, now func() time.Time) (Observation, error) {
	benchmark, digest, endpoint := evidence.benchmark, evidence.digest, evidence.endpoint
	if !statsV1ScoreIdentityComplete(row) {
		return Observation{}, errors.New("required identity or provenance field is missing")
	}
	if row.BenchmarkID != benchmark.ID {
		return Observation{}, fmt.Errorf("benchmark id %q does not match requested %q", row.BenchmarkID, benchmark.ID)
	}
	if row.ScoredAt.IsZero() || now == nil {
		return Observation{}, errors.New("score timestamp or collection clock is missing")
	}
	var score float64
	if row.NormalizedScore != nil {
		score = *row.NormalizedScore
	} else {
		if row.MaxScore == 0 {
			return Observation{}, errors.New("max_score is zero and normalized_score is missing")
		}
		score = row.Score / row.MaxScore
	}
	score *= 100
	if !finite(score) || score < 0 || score > 100 {
		return Observation{}, fmt.Errorf("normalized score %v is outside 0..100", score)
	}
	grade := aggregatorUnspecified
	if row.Verified {
		grade = aggregatorVerified
	} else if row.IsSelfReported {
		grade = aggregatorSelfReported
	}
	return Observation{
		SourceID:         llmStatsStatsV1SourceID,
		SourceRevision:   digest,
		SourceSHA256:     digest,
		Benchmark:        benchmark.ID,
		BenchmarkVersion: llmStatsStatsV1BenchmarkVersion,
		Cohort:           llmStatsStatsV1Cohort,
		ModelID:          row.ModelID,
		OrganizationID:   row.Organization,
		RawScore:         score,
		Unit:             "percent",
		Direction:        higher1,
		Methodology:      llmStatsStatsV1Methodology,
		EvidenceGrade:    grade,
		FetchedAt:        now().UTC(),
		Locator:          fmt.Sprintf("%s#row=%d&model=%s", endpoint, index, url.QueryEscape(row.ModelID)),
		SourceClass:      SourceAggregatorResult,
	}, nil
}
