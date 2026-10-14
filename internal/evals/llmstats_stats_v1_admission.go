package evals

import (
	"fmt"
)

type statsV1ScorePageEvidence struct {
	benchmark llmStatsStatsV1Benchmark
	digest    string
	endpoint  string
	page      int
}

func admitLLMStatsStatsV1ScorePage(opts Options, response llmStatsStatsV1ScoresResponse, evidence statsV1ScorePageEvidence, seenRows map[string]float64) ([]Observation, error) {
	observations := make([]Observation, 0, len(response.Scores))
	for index, row := range response.Scores {
		observation, err := llmStatsStatsV1Observation(row, evidence, index, opts.Now)
		if err != nil {
			return nil, fmt.Errorf("stats v1 benchmark %s page %d row %d: %w", evidence.benchmark.ID, evidence.page, index+1, err)
		}
		if prior, duplicate := seenRows[observation.ModelID]; duplicate {
			if prior != observation.RawScore {
				return nil, fmt.Errorf("stats v1 benchmark %s has conflicting scores for model %q", evidence.benchmark.ID, observation.ModelID)
			}
			continue
		}
		seenRows[observation.ModelID] = observation.RawScore
		observations = append(observations, observation)
	}
	return observations, nil
}
