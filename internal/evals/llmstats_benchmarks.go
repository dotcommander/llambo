package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type llmStatsBenchmark struct {
	ID         string
	Name       string
	MaxScore   *float64
	ModelCount int
}

type llmStatsBenchmarkPage struct {
	BenchmarkID string   `json:"benchmark_id"`
	MaxScore    *float64 `json:"max_score"`
	TotalModels int      `json:"total_models"`
	Entries     []struct {
		ModelID        string   `json:"model_id"`
		OrganizationID string   `json:"organization_id"`
		Score          *float64 `json:"benchmark_score"`
		Verified       bool     `json:"verified"`
		SelfReported   bool     `json:"self_reported"`
	} `json:"entries"`
}

func fetchLLMStatsBenchmarkLeads(ctx context.Context, opts Options) ([]Observation, error) {
	catalogBytes, err := getBytes(ctx, opts.Client, opts.LLMBenchmarksURL)
	if err != nil {
		return nil, fmt.Errorf("fetch LLM Stats benchmark catalog: %w", err)
	}
	catalog, err := decodeLLMStatsBenchmarkCatalog(catalogBytes)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(opts.CacheDir, "sealed", "llm-stats")
	if err := writeSealed(sealedRevisionPath(root, "catalog", catalogBytes, ".json"), catalogBytes); err != nil {
		return nil, err
	}

	type result struct {
		benchmark    string
		observations []Observation
		err          error
	}
	jobs := make(chan llmStatsBenchmark)
	results := make(chan result, len(catalog))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	workers := min(8, max(1, len(catalog)))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for benchmark := range jobs {
				observations, err := fetchLLMStatsBenchmarkPages(ctx, opts, root, benchmark)
				results <- result{benchmark: benchmark.ID, observations: observations, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, benchmark := range catalog {
			select {
			case jobs <- benchmark:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()

	all := make([]Observation, 0)
	blocked := make([]map[string]string, 0)
	var firstErr error
	for result := range results {
		if result.err != nil {
			if strings.Contains(result.err.Error(), "HTTP 404") {
				blocked = append(blocked, map[string]string{"benchmark": result.benchmark, "status": "blocked", "reason": "public result endpoint returned HTTP 404"})
				continue
			}
			if firstErr == nil {
				firstErr = result.err
				cancel()
			}
		}
		all = append(all, result.observations...)
	}
	if firstErr != nil {
		return nil, firstErr
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
	normalized, err := EncodeObservationsJSONL(all)
	if err != nil {
		return nil, err
	}
	if err := writeSealed(sealedRevisionPath(root, "observations", normalized, ".jsonl"), normalized); err != nil {
		return nil, err
	}
	if err := RebuildObservationIndex(ctx, filepath.Join(opts.CacheDir, "llm-stats-observations.sqlite"), all); err != nil {
		return nil, err
	}
	return all, nil
}

func encodeJSONLMaps(rows []map[string]string) ([]byte, error) {
	var output strings.Builder
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			return nil, err
		}
	}
	return []byte(output.String()), nil
}

func decodeLLMStatsBenchmarkCatalog(data []byte) ([]llmStatsBenchmark, error) {
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode LLM Stats benchmark catalog: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("LLM Stats benchmark catalog is empty")
	}
	result := make([]llmStatsBenchmark, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for index, row := range raw {
		var benchmark llmStatsBenchmark
		if err := requiredJSONField(row, "benchmark_id", &benchmark.ID); err != nil {
			return nil, fmt.Errorf("catalog row %d: %w", index+1, err)
		}
		if err := requiredJSONField(row, "name", &benchmark.Name); err != nil {
			return nil, fmt.Errorf("catalog row %d: %w", index+1, err)
		}
		if value := row["max_score"]; len(value) > 0 && string(value) != "null" {
			if err := json.Unmarshal(value, &benchmark.MaxScore); err != nil {
				return nil, fmt.Errorf("catalog row %d max_score: %w", index+1, err)
			}
		}
		if value := row["model_count"]; len(value) > 0 {
			if err := json.Unmarshal(value, &benchmark.ModelCount); err != nil {
				return nil, fmt.Errorf("catalog row %d model_count: %w", index+1, err)
			}
		}
		if strings.TrimSpace(benchmark.ID) == "" || strings.Contains(benchmark.ID, "/") {
			return nil, fmt.Errorf("catalog row %d has invalid benchmark_id", index+1)
		}
		if _, ok := seen[benchmark.ID]; ok {
			return nil, fmt.Errorf("duplicate benchmark_id %q", benchmark.ID)
		}
		seen[benchmark.ID] = struct{}{}
		result = append(result, benchmark)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func requiredJSONField(row map[string]json.RawMessage, name string, dst any) error {
	value, ok := row[name]
	if !ok || string(value) == "null" {
		return fmt.Errorf("missing %s", name)
	}
	if err := json.Unmarshal(value, dst); err != nil {
		return fmt.Errorf("invalid %s: %w", name, err)
	}
	return nil
}

func fetchLLMStatsBenchmarkPages(ctx context.Context, opts Options, root string, benchmark llmStatsBenchmark) ([]Observation, error) {
	observations := make([]Observation, 0, benchmark.ModelCount)
	seen := make(map[string]struct{})
	for offset := 0; ; offset += 20 {
		endpoint := strings.TrimRight(opts.LLMBenchmarksURL, "/") + "/" + url.PathEscape(benchmark.ID) + "?offset=" + strconv.Itoa(offset)
		data, err := getBytes(ctx, opts.Client, endpoint)
		if err != nil {
			return nil, fmt.Errorf("fetch benchmark %s offset %d: %w", benchmark.ID, offset, err)
		}
		var page llmStatsBenchmarkPage
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("decode benchmark %s offset %d: %w", benchmark.ID, offset, err)
		}
		if page.BenchmarkID != benchmark.ID || page.TotalModels < 0 {
			return nil, fmt.Errorf("benchmark %s pagination metadata changed at offset %d", benchmark.ID, offset)
		}
		if len(page.Entries) == 0 && len(observations) < page.TotalModels {
			return nil, fmt.Errorf("benchmark %s pagination incomplete at offset %d", benchmark.ID, offset)
		}
		digest := SealBytes(data)
		pagePath := filepath.Join(root, SealBytes([]byte(benchmark.ID))[:16], fmt.Sprintf("%08d-%s.json", offset, digest))
		if err := writeSealed(pagePath, data); err != nil {
			return nil, err
		}
		for index, entry := range page.Entries {
			if strings.TrimSpace(entry.ModelID) == "" || entry.Score == nil {
				return nil, fmt.Errorf("benchmark %s offset %d row %d is malformed", benchmark.ID, offset, index+1)
			}
			if _, ok := seen[entry.ModelID]; ok {
				return nil, fmt.Errorf("benchmark %s has duplicate model %q", benchmark.ID, entry.ModelID)
			}
			seen[entry.ModelID] = struct{}{}
			grade := "aggregator_unspecified"
			if entry.Verified {
				grade = "aggregator_verified"
			} else if entry.SelfReported {
				grade = "aggregator_self_reported"
			}
			observations = append(observations, Observation{SourceID: "llm-stats-benchmark-results", SourceRevision: digest, SourceSHA256: digest, Benchmark: benchmark.ID, BenchmarkVersion: "public-leaderboard", Cohort: "public", ModelID: entry.ModelID, OrganizationID: entry.OrganizationID, RawScore: *entry.Score, Unit: "source_native", Direction: "higher", Methodology: "LLM Stats public leaderboard aggregation; verification status supplied per row", EvidenceGrade: grade, FetchedAt: opts.Now().UTC(), Locator: fmt.Sprintf("%s#entries[%d]", endpoint, index), SourceClass: SourceAggregatorResult})
		}
		if len(observations) >= page.TotalModels {
			if len(observations) != page.TotalModels {
				return nil, fmt.Errorf("benchmark %s returned %d rows, expected %d", benchmark.ID, len(observations), page.TotalModels)
			}
			break
		}
	}
	return observations, nil
}

func attachLLMStatsBenchmarkLeads(models []Model, observations []Observation) {
	byID := make(map[string]*Model, len(models))
	for i := range models {
		byID[models[i].Key] = &models[i]
	}
	for _, observation := range observations {
		model := byID[observation.ModelID]
		if model == nil {
			continue
		}
		if model.Benchmarks == nil {
			model.Benchmarks = make(map[string]BenchmarkResult)
		}
		score := observation.RawScore
		storeBenchmarkResult(model.Benchmarks, observation.Benchmark, BenchmarkResult{Score: &score, Identity: IdentityMatchExact, Version: observation.BenchmarkVersion, ContentSHA: observation.SourceSHA256, Method: observation.Methodology, FetchedAt: observation.FetchedAt, SourceClass: string(observation.SourceClass), EvidenceGrade: observation.EvidenceGrade, Unit: observation.Unit, Direction: observation.Direction, Cohort: observation.Cohort, SampleSize: observation.SampleSize, Locator: observation.Locator, SourceID: observation.SourceID, SourceRevision: observation.SourceRevision})
	}
}

func getBytes(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "llambo-evals/1")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: HTTP %d", endpoint, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSourceBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSourceBody {
		return nil, fmt.Errorf("GET %s: response exceeds %d bytes", endpoint, maxSourceBody)
	}
	return data, nil
}

func writeSealed(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) != string(data) {
			return fmt.Errorf("sealed artifact conflict: %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sealedRevisionPath(root, name string, data []byte, extension string) string {
	return filepath.Join(root, name+"-"+SealBytes(data)+extension)
}
