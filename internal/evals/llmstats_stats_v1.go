package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Repeated string literals consolidated by goconst.
const (
	benchmark1 = "benchmark"
	blocked1   = "blocked"
	reason1    = "reason"
)

const status1 = "status"

const (
	llmStatsStatsV1SourceID                      = "llm-stats-stats-v1-scores"
	llmStatsStatsV1BenchmarkVersion              = "stats-v1"
	llmStatsStatsV1Cohort                        = "stats-v1"
	llmStatsStatsV1Methodology                   = "LLM Stats Stats v1 scores API; verification status supplied per row"
	llmStatsStatsV1ScoresPageLimit               = 500
	llmStatsStatsV1MaximumScorePagesPerBenchmark = 100
)

type llmStatsStatsV1Benchmark struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ModelCount int    `json:"model_count"`
}

type llmStatsStatsV1BenchmarksResponse struct {
	Benchmarks []llmStatsStatsV1Benchmark `json:"benchmarks"`
}

type llmStatsStatsV1ScoreRow struct {
	ModelID         string    `json:"model_id"`
	ModelName       string    `json:"model_name"`
	Organization    string    `json:"organization"`
	BenchmarkID     string    `json:"benchmark_id"`
	BenchmarkName   string    `json:"benchmark_name"`
	Category        *string   `json:"category"`
	Score           float64   `json:"score"`
	NormalizedScore *float64  `json:"normalized_score"`
	MaxScore        float64   `json:"max_score"`
	IsSelfReported  bool      `json:"is_self_reported"`
	Verified        bool      `json:"verified"`
	ScoredAt        time.Time `json:"scored_at"`
	Source          string    `json:"source"`
	URL             string    `json:"url"`
}

type llmStatsStatsV1ScoresResponse struct {
	Scores     []llmStatsStatsV1ScoreRow `json:"scores"`
	NextCursor string                    `json:"next_cursor"`
	Total      int                       `json:"total"`
}

type llmStatsStatsV1ModelProvider struct {
	ProviderID   string   `json:"provider_id"`
	ProviderName string   `json:"provider_name"`
	InputPrice   *float64 `json:"input_price_per_m"`
	OutputPrice  *float64 `json:"output_price_per_m"`
	Throughput   *float64 `json:"throughput_tps"`
	Latency      *float64 `json:"latency_s"`
	Status       string   `json:"status"`
}

type llmStatsStatsV1Model struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Organization struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"organization"`
	License struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"license"`
	OpenWeight    bool                           `json:"open_weight"`
	ModelType     string                         `json:"model_type"`
	ContextWindow *int64                         `json:"context_window"`
	Providers     []llmStatsStatsV1ModelProvider `json:"providers"`
}

type llmStatsStatsV1ModelsResponse struct {
	Models     []llmStatsStatsV1Model `json:"models"`
	NextCursor string                 `json:"next_cursor"`
	Total      int                    `json:"total"`
}

// fetchLLMStatsStatsV1Snapshot is the complete source-scoped Stats v1 refresh:
// it retains all LLM model identities and attaches sealed reviewed benchmark
// observations without invoking the legacy leaderboard discovery feeds.
func fetchLLMStatsStatsV1Snapshot(ctx context.Context, opts Options) (sourceSnapshot, error) {
	if strings.TrimSpace(opts.LLMStatsAPIKey) == "" {
		return sourceSnapshot{}, errors.New("LLM_STATS_KEY is not set")
	}
	models, modelPages, err := fetchLLMStatsStatsV1Models(ctx, opts)
	if err != nil {
		return sourceSnapshot{}, err
	}
	observations, err := fetchLLMStatsStatsV1BenchmarkLeads(ctx, opts)
	if err != nil {
		return sourceSnapshot{}, err
	}
	attachLLMStatsBenchmarkLeads(models, observations)
	normalized, err := EncodeObservationsJSONL(observations)
	if err != nil {
		return sourceSnapshot{}, err
	}
	fingerprint := make([]byte, 0)
	for _, page := range modelPages {
		fingerprint = append(fingerprint, page...)
	}
	fingerprint = append(fingerprint, normalized...)
	sort.Slice(models, func(i, j int) bool { return models[i].Key < models[j].Key })
	return sourceSnapshot{
		Models:          models,
		ContentSHA:      SealBytes(fingerprint),
		Method:          "LLM Stats Stats v1 models and reviewed benchmark scores; verification status supplied per row",
		Observations:    len(observations),
		RegistryVersion: SourceRegistryVersion,
		EvidenceGrade:   gradedAggregator,
	}, nil
}

func fetchLLMStatsStatsV1Models(ctx context.Context, opts Options) ([]Model, [][]byte, error) {
	var rows []llmStatsStatsV1Model
	var pages [][]byte
	cursor := ""
	seenCursors := make(map[string]struct{})
	for page := 1; ; page++ {
		endpoint, err := llmStatsStatsV1Endpoint(opts.LLMStatsStatsV1ModelsURL, 200, cursor)
		if err != nil {
			return nil, nil, err
		}
		if _, duplicate := seenCursors[cursor]; duplicate {
			return nil, nil, fmt.Errorf("stats v1 models repeated cursor %q", cursor)
		}
		seenCursors[cursor] = struct{}{}
		data, err := getBytesWithBearer(ctx, opts.Client, endpoint, opts.LLMStatsAPIKey)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch Stats v1 models page %d: %w", page, err)
		}
		var response llmStatsStatsV1ModelsResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, nil, fmt.Errorf("decode Stats v1 models page %d: %w", page, err)
		}
		if response.Total < 0 || (page == 1 && len(response.Models) == 0) {
			return nil, nil, fmt.Errorf("stats v1 models page %d has invalid pagination metadata", page)
		}
		if err := writeSealed(sealedRevisionPath(filepath.Join(opts.CacheDir, "sealed", "llm-stats-stats-v1"), "models", data, ".json"), data); err != nil {
			return nil, nil, err
		}
		pages = append(pages, data)
		rows = append(rows, response.Models...)
		if response.NextCursor == "" {
			break
		}
		if page >= 100 {
			return nil, nil, errors.New("stats v1 models pagination exceeded 100 pages")
		}
		cursor = response.NextCursor
	}
	models, err := projectLLMStatsStatsV1Models(rows)
	if err != nil {
		return nil, nil, err
	}
	return models, pages, nil
}

// fetchLLMStatsStatsV1BenchmarkLeads collects only reviewed portfolio benchmarks
// through the authenticated Stats v1 API. The rows are sealed immediately, but
// scoring remains governed by the existing immutable cohort contracts.
func fetchLLMStatsStatsV1BenchmarkLeads(ctx context.Context, opts Options) ([]Observation, error) {
	if strings.TrimSpace(opts.LLMStatsAPIKey) == "" {
		return nil, errors.New("LLM_STATS_KEY is not set")
	}
	catalogData, err := getBytesWithBearer(ctx, opts.Client, opts.LLMStatsStatsV1BenchmarksURL, opts.LLMStatsAPIKey)
	if err != nil {
		return nil, fmt.Errorf("fetch LLM Stats Stats v1 benchmark catalog: %w", err)
	}
	catalog, err := decodeLLMStatsStatsV1Benchmarks(catalogData)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(opts.CacheDir, "sealed", "llm-stats-stats-v1")
	if err = writeSealed(sealedRevisionPath(root, "catalog", catalogData, ".json"), catalogData); err != nil {
		return nil, err
	}

	eligible, blocked, err := eligibleLLMStatsStatsV1Benchmarks(catalog)
	if err != nil {
		return nil, err
	}
	if err = sealLLMStatsBlockedReceipts(root, blocked); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := startLLMStatsBenchmarkJobs(ctx, eligible, func(benchmark llmStatsStatsV1Benchmark) llmStatsBenchmarkJobResult {
		observations, fetchErr := fetchLLMStatsStatsV1BenchmarkScores(ctx, opts, root, benchmark)
		return llmStatsBenchmarkJobResult{benchmark: benchmark.ID, observations: observations, err: fetchErr}
	})

	all := make([]Observation, 0)
	var firstErr error
	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
				cancel()
			}
			continue
		}
		all = append(all, result.observations...)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(all, func(i, j int) bool { return observationLess(all[i], all[j]) })
	normalized, err := EncodeObservationsJSONL(all)
	if err != nil {
		return nil, err
	}
	if err := writeSealed(sealedRevisionPath(root, "observations", normalized, ".jsonl"), normalized); err != nil {
		return nil, err
	}
	if err := RebuildObservationIndex(ctx, filepath.Join(opts.CacheDir, "llm-stats-stats-v1-observations.sqlite"), all); err != nil {
		return nil, err
	}
	return all, nil
}

func decodeLLMStatsStatsV1Benchmarks(data []byte) ([]llmStatsStatsV1Benchmark, error) {
	var response llmStatsStatsV1BenchmarksResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode LLM Stats Stats v1 benchmark catalog: %w", err)
	}
	if len(response.Benchmarks) == 0 {
		return nil, errors.New("LLM Stats Stats v1 benchmark catalog is empty")
	}
	for index, benchmark := range response.Benchmarks {
		if strings.TrimSpace(benchmark.ID) == "" || strings.TrimSpace(benchmark.Name) == "" || benchmark.ModelCount < 0 {
			return nil, fmt.Errorf("stats v1 benchmark catalog row %d is malformed", index+1)
		}
	}
	sort.Slice(response.Benchmarks, func(i, j int) bool {
		return response.Benchmarks[i].ID < response.Benchmarks[j].ID
	})
	return response.Benchmarks, nil
}

func reviewedBenchmarkSet() map[string]struct{} {
	result := make(map[string]struct{})
	for _, category := range categorySpecs {
		for _, family := range category.families {
			for _, benchmark := range family.benchmarks {
				result[benchmark] = struct{}{}
			}
		}
	}
	return result
}
