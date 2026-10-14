package evals

import (
	"encoding/json"
	"fmt"
	"strings"
)

func decodeLLMStatsBenchmarkCatalogRow(row map[string]json.RawMessage, index int) (llmStatsBenchmark, error) {
	var benchmark llmStatsBenchmark
	if err := requiredJSONField(row, "benchmark_id", &benchmark.ID); err != nil {
		return llmStatsBenchmark{}, fmt.Errorf("catalog row %d: %w", index+1, err)
	}
	if err := requiredJSONField(row, "name", &benchmark.Name); err != nil {
		return llmStatsBenchmark{}, fmt.Errorf("catalog row %d: %w", index+1, err)
	}
	if value := row["max_score"]; len(value) > 0 && string(value) != "null" {
		if err := json.Unmarshal(value, &benchmark.MaxScore); err != nil {
			return llmStatsBenchmark{}, fmt.Errorf("catalog row %d max_score: %w", index+1, err)
		}
	}
	if value := row["model_count"]; len(value) > 0 {
		if err := json.Unmarshal(value, &benchmark.ModelCount); err != nil {
			return llmStatsBenchmark{}, fmt.Errorf("catalog row %d model_count: %w", index+1, err)
		}
	}
	if strings.TrimSpace(benchmark.ID) == "" || strings.Contains(benchmark.ID, "/") {
		return llmStatsBenchmark{}, fmt.Errorf("catalog row %d has invalid benchmark_id", index+1)
	}
	return benchmark, nil
}
