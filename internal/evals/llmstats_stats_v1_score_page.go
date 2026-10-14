package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

func readLLMStatsStatsV1ScorePage(ctx context.Context, opts Options, root string, evidence statsV1ScorePageEvidence) (llmStatsStatsV1ScoresResponse, statsV1ScorePageEvidence, error) {
	data, err := getBytesWithBearer(ctx, opts.Client, evidence.endpoint, opts.LLMStatsAPIKey)
	if err != nil {
		return llmStatsStatsV1ScoresResponse{}, evidence, fmt.Errorf("fetch Stats v1 benchmark %s page %d: %w", evidence.benchmark.ID, evidence.page, err)
	}
	var response llmStatsStatsV1ScoresResponse
	if err = json.Unmarshal(data, &response); err != nil {
		return response, evidence, fmt.Errorf("decode Stats v1 benchmark %s page %d: %w", evidence.benchmark.ID, evidence.page, err)
	}
	if response.Total < 0 {
		return response, evidence, fmt.Errorf("stats v1 benchmark %s page %d has invalid total", evidence.benchmark.ID, evidence.page)
	}
	evidence.digest = SealBytes(data)
	pagePath := filepath.Join(root, SealBytes([]byte(evidence.benchmark.ID))[:16], fmt.Sprintf("%04d-%s.json", evidence.page, evidence.digest))
	if err = writeSealed(pagePath, data); err != nil {
		return response, evidence, err
	}
	return response, evidence, nil
}
