package evals

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func fetchLLMStatsStatsV1BenchmarkScores(ctx context.Context, opts Options, root string, benchmark llmStatsStatsV1Benchmark) ([]Observation, error) {
	observations := make([]Observation, 0, benchmark.ModelCount)
	seenRows := make(map[string]float64, benchmark.ModelCount)
	seenCursors := make(map[string]struct{})
	cursor := ""
	for page := 1; ; page++ {
		endpoint, err := llmStatsStatsV1ScoresEndpoint(opts.LLMStatsStatsV1ScoresURL, benchmark.ID, cursor)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenCursors[cursor]; duplicate {
			return nil, fmt.Errorf("stats v1 benchmark %s repeated cursor %q", benchmark.ID, cursor)
		}
		seenCursors[cursor] = struct{}{}
		response, evidence, err := readLLMStatsStatsV1ScorePage(ctx, opts, root, statsV1ScorePageEvidence{benchmark: benchmark, endpoint: endpoint, page: page})
		if err != nil {
			return nil, err
		}
		rows, err := admitLLMStatsStatsV1ScorePage(opts, response, evidence, seenRows)
		if err != nil {
			return nil, err
		}
		observations = append(observations, rows...)
		if response.NextCursor == "" {
			break
		}
		if page >= llmStatsStatsV1MaximumScorePagesPerBenchmark {
			return nil, fmt.Errorf("stats v1 benchmark %s pagination exceeded %d pages", benchmark.ID, llmStatsStatsV1MaximumScorePagesPerBenchmark)
		}
		cursor = response.NextCursor
	}
	if err := validateLLMStatsStatsV1BenchmarkPopulation(benchmark, observations); err != nil {
		return nil, err
	}
	return observations, nil
}

func validateLLMStatsStatsV1BenchmarkPopulation(benchmark llmStatsStatsV1Benchmark, observations []Observation) error {
	if len(observations) == 0 {
		return fmt.Errorf("stats v1 benchmark %s returned no unique score rows", benchmark.ID)
	}
	if len(observations) != benchmark.ModelCount {
		observed := make([]string, 0, len(observations))
		for _, observation := range observations {
			observed = append(observed, observation.ModelID)
		}
		return fmt.Errorf("stats v1 benchmark %s returned %d unique models, catalog declared %d (observed: %s)", benchmark.ID, len(observations), benchmark.ModelCount, strings.Join(observed, ", "))
	}
	return nil
}

func llmStatsStatsV1ScoresEndpoint(base, benchmark, cursor string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid LLM Stats Stats v1 scores URL %q", base)
	}
	query := parsed.Query()
	query.Set(benchmark1, benchmark)
	query.Set("limit", strconv.Itoa(llmStatsStatsV1ScoresPageLimit))
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func llmStatsStatsV1Endpoint(base string, limit int, cursor string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid LLM Stats Stats v1 URL %q", base)
	}
	query := parsed.Query()
	query.Set("limit", strconv.Itoa(limit))
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
