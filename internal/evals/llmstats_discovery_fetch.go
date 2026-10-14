package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

type llmStatsDiscovery struct {
	identities []llmIdentity
	full       []llmFull
	indexes    map[string]indexEnvelope
	raw        map[string][]byte
}

func acquireLLMStatsDiscovery(ctx context.Context, opts Options) (llmStatsDiscovery, error) {
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
			return llmStatsDiscovery{}, result.err
		}
		raw[result.name] = result.data
	}
	if err := json.Unmarshal(raw["models"], &identities); err != nil {
		return llmStatsDiscovery{}, fmt.Errorf("decode LLM Stats models: %w", err)
	}
	if err := json.Unmarshal(raw["full-results"], &full); err != nil {
		return llmStatsDiscovery{}, fmt.Errorf("decode LLM Stats full results: %w", err)
	}
	if err := json.Unmarshal(raw["indexes"], &indexes); err != nil {
		return llmStatsDiscovery{}, fmt.Errorf("decode LLM Stats indexes: %w", err)
	}
	sealedRoot := filepath.Join(opts.CacheDir, "sealed", llmStats)
	for _, name := range []string{"models", "full-results", "indexes"} {
		if err := writeSealed(sealedRevisionPath(sealedRoot, name, raw[name], ".json"), raw[name]); err != nil {
			return llmStatsDiscovery{}, err
		}
	}

	return llmStatsDiscovery{identities: identities, full: full, indexes: indexes, raw: raw}, nil
}
