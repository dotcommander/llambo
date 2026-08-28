package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type llmIdentity struct {
	ModelID      string   `json:"model_id"`
	Name         string   `json:"name"`
	ModelType    string   `json:"model_type"`
	Organization string   `json:"organization"`
	License      string   `json:"license"`
	IsOpen       bool     `json:"is_open"`
	Context      *int64   `json:"context"`
	InputPrice   *float64 `json:"input_price"`
	OutputPrice  *float64 `json:"output_price"`
}

type llmFull struct {
	ModelID     string   `json:"model_id"`
	Context     *int64   `json:"context"`
	InputPrice  *float64 `json:"input_price"`
	OutputPrice *float64 `json:"output_price"`
	Throughput  *float64 `json:"throughput"`
	Latency     *float64 `json:"latency"`
	GPQA        *float64 `json:"gpqa_score"`
	SWEVerified *float64 `json:"swe_bench_verified_score"`
	SWEPro      *float64 `json:"swe_bench_pro_score"`
	SciCode     *float64 `json:"scicode_score"`
	MCPAtlas    *float64 `json:"mcp_atlas_score"`
}

type indexEnvelope struct {
	Models []struct {
		ModelID      string  `json:"model_id"`
		Conservative float64 `json:"conservative"`
		Mu           float64 `json:"mu"`
		Sigma        float64 `json:"sigma"`
		Rank         int     `json:"rank"`
		GamesPlayed  int     `json:"games_played"`
	} `json:"models"`
}

func fetchLLMStats(ctx context.Context, opts Options) (sourceSnapshot, error) {
	if strings.TrimSpace(opts.LLMStatsAPIKey) != "" && opts.IngestLLMBenchmarks {
		return fetchLLMStatsStatsV1Snapshot(ctx, opts)
	}
	var identities []llmIdentity
	var full []llmFull
	indexes := map[string]indexEnvelope{}
	type payload struct {
		name string
		data []byte
		err  error
	}
	payloads := make(chan payload, 3)
	for name, endpoint := range map[string]string{"models": opts.LLMModelsURL, "full-results": opts.LLMFullURL, "indexes": opts.LLMIndexURL} {
		go func() {
			data, err := getBytes(ctx, opts.Client, endpoint)
			payloads <- payload{name: name, data: data, err: err}
		}()
	}
	raw := make(map[string][]byte, 3)
	for range 3 {
		result := <-payloads
		if result.err != nil {
			return sourceSnapshot{}, result.err
		}
		raw[result.name] = result.data
	}
	if err := json.Unmarshal(raw["models"], &identities); err != nil {
		return sourceSnapshot{}, fmt.Errorf("decode LLM Stats models: %w", err)
	}
	if err := json.Unmarshal(raw["full-results"], &full); err != nil {
		return sourceSnapshot{}, fmt.Errorf("decode LLM Stats full results: %w", err)
	}
	if err := json.Unmarshal(raw["indexes"], &indexes); err != nil {
		return sourceSnapshot{}, fmt.Errorf("decode LLM Stats indexes: %w", err)
	}
	sealedRoot := filepath.Join(opts.CacheDir, "sealed", "llm-stats")
	for _, name := range []string{"models", "full-results", "indexes"} {
		if err := writeSealed(sealedRevisionPath(sealedRoot, name, raw[name], ".json"), raw[name]); err != nil {
			return sourceSnapshot{}, err
		}
	}

	fullByID := make(map[string]llmFull, len(full))
	for _, row := range full {
		fullByID[row.ModelID] = row
	}
	indexByID := map[string]map[string]Index{}
	for category, envelope := range indexes {
		for _, row := range envelope.Models {
			if indexByID[row.ModelID] == nil {
				indexByID[row.ModelID] = map[string]Index{}
			}
			indexByID[row.ModelID][category] = Index{Conservative: row.Conservative, Mu: row.Mu, Sigma: row.Sigma, Rank: row.Rank, GamesPlayed: row.GamesPlayed}
		}
	}
	models := make([]Model, 0, len(identities))
	for _, identity := range identities {
		if identity.ModelType != "llm" {
			continue
		}
		f := fullByID[identity.ModelID]
		context := identity.Context
		if context == nil {
			context = f.Context
		}
		input, output := identity.InputPrice, identity.OutputPrice
		if input == nil {
			input = f.InputPrice
		}
		if output == nil {
			output = f.OutputPrice
		}
		models = append(models, Model{Key: identity.ModelID, Name: identity.Name, Organization: identity.Organization, License: identity.License, Open: boolPtr(identity.IsOpen), Context: context,
			LLMStats: &LLMStatsMetrics{InputPrice: input, OutputPrice: output, Throughput: f.Throughput, Latency: f.Latency, GPQA: f.GPQA, SWEVerified: f.SWEVerified, SWEPro: f.SWEPro, SciCode: f.SciCode, MCPAtlas: f.MCPAtlas, Indexes: indexByID[identity.ModelID]}})
	}
	observationCount := 0
	method := "LLM Stats public discovery feeds; published benchmark rows use graded verification evidence"
	fingerprintInput := append(append(append([]byte(nil), raw["models"]...), raw["full-results"]...), raw["indexes"]...)
	if opts.IngestLLMBenchmarks {
		var observations []Observation
		var err error
		if strings.TrimSpace(opts.LLMStatsAPIKey) != "" {
			observations, err = fetchLLMStatsStatsV1BenchmarkLeads(ctx, opts)
			method = "LLM Stats public discovery feeds plus Stats v1 benchmark scores; published rows use graded verification evidence"
		} else {
			observations, err = fetchLLMStatsBenchmarkLeads(ctx, opts)
		}
		if err != nil {
			return sourceSnapshot{}, err
		}
		attachLLMStatsBenchmarkLeads(models, observations)
		observationCount = len(observations)
		normalized, err := EncodeObservationsJSONL(observations)
		if err != nil {
			return sourceSnapshot{}, err
		}
		fingerprintInput = append(fingerprintInput, normalized...)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Key < models[j].Key })
	return sourceSnapshot{Models: models, ContentSHA: SealBytes(fingerprintInput), Method: method, Observations: observationCount, RegistryVersion: SourceRegistryVersion, EvidenceGrade: "graded_aggregator"}, nil
}

type aaEnvelope struct {
	IndexVersion float64 `json:"intelligence_index_version"`
	Pagination   struct {
		Page       int  `json:"page"`
		TotalPages int  `json:"total_pages"`
		HasMore    bool `json:"has_more"`
	} `json:"pagination"`
	Data []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Slug    string `json:"slug"`
		Creator struct {
			Name string `json:"name"`
		} `json:"model_creator"`
		Evaluations struct {
			Intelligence *float64 `json:"artificial_analysis_intelligence_index"`
			Coding       *float64 `json:"artificial_analysis_coding_index"`
			Agentic      *float64 `json:"artificial_analysis_agentic_index"`
		} `json:"evaluations"`
		Pricing struct {
			Input  *float64 `json:"price_1m_input_tokens"`
			Output *float64 `json:"price_1m_output_tokens"`
		} `json:"pricing"`
		Performance struct {
			OutputTPS *float64 `json:"median_output_tokens_per_second"`
			TTFT      *float64 `json:"median_time_to_first_token_seconds"`
			E2E       *float64 `json:"median_end_to_end_response_time_seconds"`
		} `json:"performance"`
	} `json:"data"`
}

func fetchArtificialAnalysis(ctx context.Context, opts Options) (sourceSnapshot, error) {
	var models []Model
	var version float64
	for page := 1; ; page++ {
		var envelope aaEnvelope
		url := opts.AAURL + "?page=" + strconv.Itoa(page)
		if err := getJSON(ctx, opts.Client, url, opts.AAAPIKey, &envelope); err != nil {
			return sourceSnapshot{}, err
		}
		if page == 1 {
			version = envelope.IndexVersion
		} else if envelope.IndexVersion != version {
			return sourceSnapshot{}, errors.New("Artificial Analysis index version changed during pagination")
		}
		for _, row := range envelope.Data {
			key := row.Slug
			if key == "" {
				key = row.ID
			}
			models = append(models, Model{Key: key, Name: row.Name, Organization: row.Creator.Name, AA: &ArtificialMetrics{ID: row.ID, Slug: row.Slug, Intelligence: row.Evaluations.Intelligence, Coding: row.Evaluations.Coding, Agentic: row.Evaluations.Agentic, InputPrice: row.Pricing.Input, OutputPrice: row.Pricing.Output, OutputTokensPS: row.Performance.OutputTPS, TTFTSeconds: row.Performance.TTFT, E2ESeconds: row.Performance.E2E}})
		}
		if !envelope.Pagination.HasMore || (envelope.Pagination.TotalPages > 0 && page >= envelope.Pagination.TotalPages) {
			break
		}
		if page >= 100 {
			return sourceSnapshot{}, errors.New("Artificial Analysis pagination exceeded 100 pages")
		}
	}
	return sourceSnapshot{Models: models, AAVersion: version}, nil
}

func getJSON(ctx context.Context, client *http.Client, url, apiKey string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "llambo-evals/1")
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	if err := decodeBoundedJSON(resp.Body, maxSourceBody, dst); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}

func decodeBoundedJSON(reader io.Reader, maxBytes int64, dst any) error {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("response exceeds %d bytes", maxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("response contains multiple JSON values")
		}
		return fmt.Errorf("response contains trailing data: %w", err)
	}
	return nil
}

func normalizeHTTPETag(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "W/") {
		value = strings.TrimSpace(strings.TrimPrefix(value, "W/"))
	}
	return strings.Trim(value, `"`)
}
