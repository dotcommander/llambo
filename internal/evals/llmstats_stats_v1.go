package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

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
		EvidenceGrade:   "graded_aggregator",
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
			return nil, nil, fmt.Errorf("Stats v1 models repeated cursor %q", cursor)
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
			return nil, nil, fmt.Errorf("Stats v1 models page %d has invalid pagination metadata", page)
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
			return nil, nil, errors.New("Stats v1 models pagination exceeded 100 pages")
		}
		cursor = response.NextCursor
	}
	models := make([]Model, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.Name) == "" || strings.TrimSpace(row.Organization.Name) == "" {
			return nil, nil, errors.New("Stats v1 model row is malformed")
		}
		if row.ModelType != "llm" {
			continue
		}
		if _, duplicate := seen[row.ID]; duplicate {
			return nil, nil, fmt.Errorf("Stats v1 models has duplicate id %q", row.ID)
		}
		seen[row.ID] = struct{}{}
		open := row.OpenWeight
		var input, output, throughput, latency *float64
		if len(row.Providers) > 0 {
			input, output, throughput, latency = row.Providers[0].InputPrice, row.Providers[0].OutputPrice, row.Providers[0].Throughput, row.Providers[0].Latency
		}
		models = append(models, Model{
			Key: row.ID, Name: row.Name, Organization: row.Organization.Name,
			License: row.License.Name, Open: &open, Context: row.ContextWindow,
			LLMStats: &LLMStatsMetrics{InputPrice: input, OutputPrice: output, Throughput: throughput, Latency: latency},
		})
	}
	if len(models) == 0 {
		return nil, nil, errors.New("Stats v1 models returned no LLM rows")
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
	if err := writeSealed(sealedRevisionPath(root, "catalog", catalogData, ".json"), catalogData); err != nil {
		return nil, err
	}

	reviewed := reviewedBenchmarkSet()
	eligible := make([]llmStatsStatsV1Benchmark, 0, len(catalog))
	blocked := make([]map[string]string, 0)
	seen := make(map[string]struct{}, len(catalog))
	for _, benchmark := range catalog {
		if _, ok := seen[benchmark.ID]; ok {
			return nil, fmt.Errorf("duplicate Stats v1 benchmark id %q", benchmark.ID)
		}
		seen[benchmark.ID] = struct{}{}
		if _, ok := reviewed[benchmark.ID]; !ok {
			continue
		}
		if benchmark.ModelCount < 5 {
			blocked = append(blocked, map[string]string{
				"benchmark": benchmark.ID,
				"status":    "blocked",
				"reason":    fmt.Sprintf("Stats v1 catalog has %d models, want at least 5", benchmark.ModelCount),
			})
			continue
		}
		eligible = append(eligible, benchmark)
	}
	for name := range reviewed {
		if _, ok := seen[name]; !ok {
			blocked = append(blocked, map[string]string{
				"benchmark": name,
				"status":    "blocked",
				"reason":    "Stats v1 catalog does not contain this reviewed benchmark",
			})
		}
	}
	sort.Slice(blocked, func(i, j int) bool { return blocked[i]["benchmark"] < blocked[j]["benchmark"] })
	if len(blocked) > 0 {
		receipts, err := encodeJSONLMaps(blocked)
		if err != nil {
			return nil, err
		}
		if err := writeSealed(sealedRevisionPath(root, "blocked", receipts, ".jsonl"), receipts); err != nil {
			return nil, err
		}
	}

	type result struct {
		benchmark    string
		observations []Observation
		err          error
	}
	jobs := make(chan llmStatsStatsV1Benchmark)
	results := make(chan result, len(eligible))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	workers := min(8, max(1, len(eligible)))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for benchmark := range jobs {
				observations, err := fetchLLMStatsStatsV1BenchmarkScores(ctx, opts, root, benchmark)
				results <- result{benchmark: benchmark.ID, observations: observations, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, benchmark := range eligible {
			select {
			case jobs <- benchmark:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()

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
			return nil, fmt.Errorf("Stats v1 benchmark catalog row %d is malformed", index+1)
		}
	}
	sort.Slice(response.Benchmarks, func(i, j int) bool {
		return response.Benchmarks[i].ID < response.Benchmarks[j].ID
	})
	return response.Benchmarks, nil
}

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
			return nil, fmt.Errorf("Stats v1 benchmark %s repeated cursor %q", benchmark.ID, cursor)
		}
		seenCursors[cursor] = struct{}{}
		data, err := getBytesWithBearer(ctx, opts.Client, endpoint, opts.LLMStatsAPIKey)
		if err != nil {
			return nil, fmt.Errorf("fetch Stats v1 benchmark %s page %d: %w", benchmark.ID, page, err)
		}
		var response llmStatsStatsV1ScoresResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("decode Stats v1 benchmark %s page %d: %w", benchmark.ID, page, err)
		}
		if response.Total < 0 {
			return nil, fmt.Errorf("Stats v1 benchmark %s page %d has invalid total", benchmark.ID, page)
		}
		digest := SealBytes(data)
		pagePath := filepath.Join(root, SealBytes([]byte(benchmark.ID))[:16], fmt.Sprintf("%04d-%s.json", page, digest))
		if err := writeSealed(pagePath, data); err != nil {
			return nil, err
		}
		for index, row := range response.Scores {
			observation, err := llmStatsStatsV1Observation(row, benchmark, digest, endpoint, index, opts.Now)
			if err != nil {
				return nil, fmt.Errorf("Stats v1 benchmark %s page %d row %d: %w", benchmark.ID, page, index+1, err)
			}
			if prior, duplicate := seenRows[observation.ModelID]; duplicate {
				if prior != observation.RawScore {
					return nil, fmt.Errorf("Stats v1 benchmark %s has conflicting scores for model %q", benchmark.ID, observation.ModelID)
				}
				continue
			}
			seenRows[observation.ModelID] = observation.RawScore
			observations = append(observations, observation)
		}
		if response.NextCursor == "" {
			break
		}
		if page >= llmStatsStatsV1MaximumScorePagesPerBenchmark {
			return nil, fmt.Errorf("Stats v1 benchmark %s pagination exceeded %d pages", benchmark.ID, llmStatsStatsV1MaximumScorePagesPerBenchmark)
		}
		cursor = response.NextCursor
	}
	if len(observations) == 0 {
		return nil, fmt.Errorf("Stats v1 benchmark %s returned no unique score rows", benchmark.ID)
	}
	if len(observations) != benchmark.ModelCount {
		return nil, fmt.Errorf("Stats v1 benchmark %s returned %d unique models, catalog declared %d", benchmark.ID, len(observations), benchmark.ModelCount)
	}
	return observations, nil
}

func llmStatsStatsV1ScoresEndpoint(base, benchmark, cursor string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid LLM Stats Stats v1 scores URL %q", base)
	}
	query := parsed.Query()
	query.Set("benchmark", benchmark)
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

func llmStatsStatsV1Observation(row llmStatsStatsV1ScoreRow, benchmark llmStatsStatsV1Benchmark, digest, endpoint string, index int, now func() time.Time) (Observation, error) {
	if strings.TrimSpace(row.ModelID) == "" || strings.TrimSpace(row.ModelName) == "" || strings.TrimSpace(row.Organization) == "" ||
		strings.TrimSpace(row.BenchmarkName) == "" || strings.TrimSpace(row.Source) == "" || strings.TrimSpace(row.URL) == "" {
		return Observation{}, errors.New("required identity or provenance field is missing")
	}
	if row.BenchmarkID != benchmark.ID {
		return Observation{}, fmt.Errorf("benchmark id %q does not match requested %q", row.BenchmarkID, benchmark.ID)
	}
	if row.ScoredAt.IsZero() || now == nil {
		return Observation{}, errors.New("score timestamp or collection clock is missing")
	}
	score := row.Score
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
	grade := "aggregator_unspecified"
	if row.Verified {
		grade = "aggregator_verified"
	} else if row.IsSelfReported {
		grade = "aggregator_self_reported"
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
		Direction:        "higher",
		Methodology:      llmStatsStatsV1Methodology,
		EvidenceGrade:    grade,
		FetchedAt:        now().UTC(),
		Locator:          fmt.Sprintf("%s#row=%d&model=%s", endpoint, index, url.QueryEscape(row.ModelID)),
		SourceClass:      SourceAggregatorResult,
	}, nil
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
