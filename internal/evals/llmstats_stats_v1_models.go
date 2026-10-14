package evals

import (
	"errors"
	"fmt"
	"strings"
)

func projectLLMStatsStatsV1Models(rows []llmStatsStatsV1Model) ([]Model, error) {
	models := make([]Model, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.Name) == "" || strings.TrimSpace(row.Organization.Name) == "" {
			return nil, errors.New("stats v1 model row is malformed")
		}
		if row.ModelType != llm1 {
			continue
		}
		if _, duplicate := seen[row.ID]; duplicate {
			return nil, fmt.Errorf("stats v1 models has duplicate id %q", row.ID)
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
		return nil, errors.New("stats v1 models returned no LLM rows")
	}
	return models, nil
}
