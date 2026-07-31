package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/providers"
)

func printPingPlan(out io.Writer, runCtx pingRunContext) {
	fmt.Fprintf(out, "Pinging %d provider/model targets in parallel with prompt: %q\n\n", len(runCtx.Targets), runCtx.Prompt)
}

func executePingTargets(targets []pingTarget, prompt string) []PingResult {
	return executePingTargetsContext(context.Background(), targets, prompt)
}

func executePingTargetsContext(ctx context.Context, targets []pingTarget, prompt string) []PingResult {
	return runOrderedProviderGroups(targets, func(target pingTarget) string { return target.Name }, func(_ int, target pingTarget) PingResult {
		timeout := pingTimeoutForProvider(target.Name)
		result := pingProviderContext(ctx, target.Name, target.Config, prompt, timeout)
		result.CostStatus = string(target.CostStatus)
		result.InputCostPer1M = target.InputCost
		result.OutputCostPer1M = target.OutputCost
		return result
	})
}

func recordPingRoutingMetrics(results []PingResult, routing providers.RoutingConfig) error {
	store, err := providers.NewRoutingMetricsStore(routing.MetricsPath)
	if err != nil {
		return fmt.Errorf("load routing metrics: %w", err)
	}
	for _, result := range results {
		recordPingResultMetric(store, result.Provider, result)
		if strings.TrimSpace(strings.ToLower(routing.CatalogModels)) == "pinned" {
			recordPingResultMetric(store, catalogBackendName(result.Provider, result.Model), result)
		}
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("save routing metrics: %w", err)
	}
	// The caller reports the updated path through its owned writer.
	return nil
}

func recordPingCatalogHealth(results []PingResult) error {
	if len(results) == 0 {
		return nil
	}
	catPath, err := catalog.CatalogPath()
	if err != nil {
		return err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	now := time.Now().UTC()
	for _, result := range results {
		catalog.RecordPing(cat, result.Provider, result.Model, result.Success, result.Latency, result.TokensIn, result.TokensOut, result.Error, now)
	}
	if err := catalog.Save(catPath, cat); err != nil {
		return fmt.Errorf("save catalog health: %w", err)
	}
	return nil
}

func recordPingResultMetric(store *providers.RoutingMetricsStore, provider string, result PingResult) {
	var err error
	if !result.Success {
		err = errors.New(result.Error)
	}
	store.Record(provider, result.Latency, pingUsage(result), err)
	if result.Success {
		store.RecordQuality(provider, pingQualityScore(result))
	}
}

func pingUsage(result PingResult) *providers.LLMUsage {
	total := result.TokensIn + result.TokensOut
	if total <= 0 {
		return nil
	}
	return &providers.LLMUsage{
		PromptTokens:     result.TokensIn,
		CompletionTokens: result.TokensOut,
		TotalTokens:      total,
	}
}

func pingQualityScore(result PingResult) float64 {
	return providers.ComputeResponseQuality(0, len(result.Response), result.Latency.Milliseconds())
}

func writePingOutput(path string, results []PingResult) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
