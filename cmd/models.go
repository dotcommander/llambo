package cmd

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

var modelsAll bool
var modelsCSV bool
var modelsAvailable bool
var modelsTimeoutSec int
var modelsGrouped = true
var modelsProviderFilter string
var modelsMetrics bool

type modelRow struct {
	Provider         string
	Enabled          bool
	Model            string
	Primary          bool
	LlamboScore      string
	LlamboProvenance string
	TaskScore        string
	Speed            string
	Latency          string
	OutputCost       string
}

func runModels(cmd *commandIO, args []string) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}

	var metricsCatalog *catalog.Catalog
	var costMap map[string]costs.ModelCost
	var scoreSnapshot *evals.OMLXScoreSnapshot
	if modelsMetrics {
		catPath, err := catalog.CatalogPath()
		if err != nil {
			return err
		}
		metricsCatalog, err = catalog.Load(catPath)
		if err != nil {
			return err
		}
		costMap, err = costs.LoadAll()
		if err != nil {
			return err
		}
		snapshotPath, err := evals.OMLXScoreSnapshotPath()
		if err != nil {
			return err
		}
		scoreSnapshot, err = evals.LoadOMLXScoreSnapshot(snapshotPath)
		if err != nil {
			return err
		}
	}

	rows := make([]modelRow, 0)
	for _, name := range sortedProviderNames(cfg.Providers) {
		if !modelProviderAllowed(name) {
			continue
		}
		pcfg := cfg.Providers[name]
		if !modelsAll && !pcfg.Enabled {
			continue
		}

		models := modelVariants(pcfg)
		if modelsAvailable {
			if avail := fetchAvailableModelsContext(cmd.Context(), name, pcfg, modelsTimeoutSec); len(avail) > 0 {
				models = avail
			}
		} else {
			models = appendCatalogMetricModels(metricsCatalog, scoreSnapshot, name, models)
		}

		for i, model := range models {
			row := modelRow{
				Provider: name,
				Enabled:  pcfg.Enabled,
				Model:    model,
				Primary:  i == 0 && !modelsAvailable,
			}
			if modelsMetrics {
				row.LlamboScore, row.LlamboProvenance, row.TaskScore, row.Speed, row.Latency = modelMetricLabels(metricsCatalog, scoreSnapshot, name, model)
				row.OutputCost = modelOutputCostLabel(costMap, metricsCatalog, name, model)
			}
			rows = append(rows, row)
		}
	}

	if modelsCSV && modelsGrouped && !modelsMetrics {
		w := csv.NewWriter(out)
		if err := w.Write([]string{"provider", "enabled", "models"}); err != nil {
			return err
		}
		for _, grouped := range groupRows(rows) {
			if err := w.Write([]string{grouped.Provider, fmt.Sprintf("%t", grouped.Enabled), strings.Join(grouped.Models, ", ")}); err != nil {
				return err
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return err
		}
		if len(rows) == 0 && !modelsAll {
			fmt.Fprintln(errOut, "No enabled providers found. Use --all to list disabled providers.")
		}
		return nil
	}

	if modelsCSV {
		header := []string{"provider", "enabled", "model", "primary"}
		if modelsMetrics {
			header = append(header, "llambo_score", "llambo_provenance", "task_score", "speed", "latency", "output_cost_per_1m_usd")
		}
		w := csv.NewWriter(out)
		if err := w.Write(header); err != nil {
			return err
		}
		for _, row := range rows {
			values := []string{row.Provider, fmt.Sprintf("%t", row.Enabled), row.Model, fmt.Sprintf("%t", row.Primary)}
			if modelsMetrics {
				values = append(values, row.LlamboScore, row.LlamboProvenance, row.TaskScore, row.Speed, row.Latency, row.OutputCost)
			}
			if err := w.Write(values); err != nil {
				return err
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return err
		}
		if len(rows) == 0 && !modelsAll {
			fmt.Fprintln(errOut, "No enabled providers found. Use --all to list disabled providers.")
		}
		return nil
	}

	if modelsGrouped && !modelsMetrics {
		fmt.Fprintf(out, "%-12s %-8s %s\n", "PROVIDER", "ENABLED", "MODELS")
		fmt.Fprintln(out, "--------------------------------------------------------------------------------")
		for _, grouped := range groupRows(rows) {
			fmt.Fprintf(out, "%-12s %-8t %s\n", grouped.Provider, grouped.Enabled, strings.Join(grouped.Models, ", "))
		}
	} else if modelsMetrics {
		fmt.Fprintf(out, "%-12s %-8s %-18s %-30s %-22s %-14s %-12s %-14s %s\n", "PROVIDER", "ENABLED", "LLAMBO SCORE", "LLAMBO PROVENANCE", "TASK SCORE", "SPEED", "LATENCY", "OUTPUT $/1M", "MODEL")
		fmt.Fprintln(out, strings.Repeat("-", 236))
		for _, row := range rows {
			fmt.Fprintf(out, "%-12s %-8t %-18s %-30s %-22s %-14s %-12s %-14s %s\n", row.Provider, row.Enabled, row.LlamboScore, row.LlamboProvenance, row.TaskScore, row.Speed, row.Latency, row.OutputCost, row.Model)
		}
	} else {
		fmt.Fprintf(out, "%-12s %-8s %-8s %s\n", "PROVIDER", "ENABLED", "PRIMARY", "MODEL")
		fmt.Fprintln(out, "--------------------------------------------------------------------------------")
		for _, row := range rows {
			fmt.Fprintf(out, "%-12s %-8t %-8t %s\n", row.Provider, row.Enabled, row.Primary, row.Model)
		}
	}

	if len(rows) == 0 {
		if modelsAll {
			fmt.Fprintln(out, "No providers found in config.")
		} else {
			fmt.Fprintln(out, "No enabled providers found. Use --all to list disabled providers.")
		}
	}

	return nil
}

func modelOutputCostLabel(costMap map[string]costs.ModelCost, cat *catalog.Catalog, provider, model string) string {
	var entry *catalog.ModelEntry
	if cat != nil {
		if pc := cat.Providers[provider]; pc != nil {
			entry = pc.Models[model]
		}
	}

	status, _, output := catalog.CostForEntry(costMap, provider, model, entry)
	switch status {
	case catalog.CostFree:
		return "free"
	case catalog.CostPaid:
		return fmt.Sprintf("$%.4g", output)
	default:
		return "—"
	}
}

func modelProviderAllowed(name string) bool {
	filter := strings.TrimSpace(modelsProviderFilter)
	if filter == "" {
		return true
	}
	for _, candidate := range strings.Split(filter, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), name) {
			return true
		}
	}
	return false
}

func modelMetricLabels(cat *catalog.Catalog, snapshot *evals.OMLXScoreSnapshot, provider, model string) (llamboScore, llamboProvenance, taskScore, speed, latency string) {
	llamboScore, llamboProvenance, taskScore, speed, latency = "—", "—", "—", "—", "—"
	if provider == "omlx" && snapshot != nil && snapshot.Contains(model) {
		scores := snapshot.CategoryScores[model]
		llamboProvenance = formatLlamboProvenance(scores, snapshot)
		llamboScore = formatLlamboCategories(scores)
	}
	if cat == nil {
		return llamboScore, llamboProvenance, taskScore, speed, latency
	}
	pc := cat.Providers[provider]
	if pc == nil {
		return llamboScore, llamboProvenance, taskScore, speed, latency
	}
	entry := pc.Models[model]
	if entry == nil {
		return llamboScore, llamboProvenance, taskScore, speed, latency
	}
	benchmarkName, benchmark, hasBenchmark := catalog.BestBenchmarkEvidence(entry)
	_ = benchmarkName // benchmark metrics remain operational evidence only.
	if task, evidence, ok := catalog.BestQualityEvidence(entry); ok {
		// This stays task-labelled by contract and is never an overall input.
		taskScore = fmt.Sprintf("%s %.1f/100", task, evidence.Score*100)
	}
	if hasBenchmark {
		if benchmark.LatencyMS > 0 {
			latency = fmt.Sprintf("%dms", benchmark.LatencyMS)
		}
		if benchmark.SpeedTokensPerSecond > 0 {
			speed = fmt.Sprintf("%.1f tok/s", benchmark.SpeedTokensPerSecond)
		}
	}
	lastPingIsNewer := entry.LastPing.Success && !entry.LastPing.CheckedAt.IsZero() &&
		(!hasBenchmark || entry.LastPing.CheckedAt.After(benchmark.UpdatedAt))
	if lastPingIsNewer {
		if entry.LastPing.LatencyMS > 0 {
			latency = fmt.Sprintf("%dms", entry.LastPing.LatencyMS)
		}
		if entry.LastPing.SpeedTokensPerSecond > 0 {
			speed = fmt.Sprintf("%.1f tok/s", entry.LastPing.SpeedTokensPerSecond)
		}
	}
	if entry.LastPing.LatencyMS > 0 {
		if latency == "—" {
			latency = fmt.Sprintf("%dms", entry.LastPing.LatencyMS)
		}
		if speed == "—" && entry.LastPing.SpeedTokensPerSecond > 0 {
			speed = fmt.Sprintf("%.1f tok/s", entry.LastPing.SpeedTokensPerSecond)
		}
	}
	return llamboScore, llamboProvenance, taskScore, speed, latency
}

func formatLlamboCategories(scores map[string]*evals.LlamboScore) string {
	if len(scores) == 0 {
		return "unresolved"
	}
	parts := make([]string, 0, len(scores))
	for _, category := range []string{"agents", "coding", "instruction-following", "long-context", "reasoning", "writing"} {
		score := scores[category]
		if score == nil {
			continue
		}
		label := fmt.Sprintf("%s=%.1f", category, score.Score)
		if score.WinnerStatus != "" {
			label += " (" + score.WinnerStatus + ")"
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "unresolved"
	}
	return strings.Join(parts, ", ")
}

func formatLlamboProvenance(scores map[string]*evals.LlamboScore, snapshot *evals.OMLXScoreSnapshot) string {
	if snapshot == nil {
		return "unresolved; snapshot missing"
	}
	parts := []string{"formula=" + snapshot.FormulaVersion, "cache_fingerprint=" + snapshot.PopulationFingerprint}
	for _, category := range []string{"agents", "coding", "instruction-following", "long-context", "reasoning", "writing"} {
		score := scores[category]
		if score == nil {
			parts = append(parts, category+"=unresolved")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s={coverage=%.6f,trusted_coverage=%.6f,confidence=%s,winner=%s,stale=%t}", category, score.Coverage, score.TrustedCoverage, score.Confidence, score.WinnerStatus, score.Stale))
	}
	return strings.Join(parts, "; ")
}

// appendCatalogMetricModels adds catalog-only models that have saved benchmark
// or quality evidence. The normal list remains immediate and config-driven for
// unmeasured models, while saved metrics are not hidden just because a model is
// no longer in the provider's preferred config model list.
func appendCatalogMetricModels(cat *catalog.Catalog, snapshot *evals.OMLXScoreSnapshot, provider string, models []string) []string {

	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		seen[model] = struct{}{}
	}
	additional := make([]string, 0)
	if provider == "omlx" {
		if snapshot == nil {
			return models
		}
		for _, model := range snapshot.Inventory {
			if _, exists := seen[model]; !exists {
				additional = append(additional, model)
			}
		}
		sort.Strings(additional)
		return append(models, additional...)
	}
	if cat == nil {
		return models
	}
	pc := cat.Providers[provider]
	if pc == nil || len(pc.Models) == 0 {
		return models
	}
	for model, entry := range pc.Models {
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		if benchmarkName, benchmark, ok := catalog.BestBenchmarkEvidence(entry); ok && benchmarkName != "" && (benchmark.Score > 0 || benchmark.LatencyMS > 0 || benchmark.SpeedTokensPerSecond > 0) {
			additional = append(additional, model)
			continue
		}
		if _, quality, ok := catalog.BestQualityEvidence(entry); ok && quality.Score > 0 {
			additional = append(additional, model)
		}
	}
	sort.Strings(additional)
	return append(models, additional...)
}

type groupedModelRow struct {
	Provider string
	Enabled  bool
	Models   []string
}

func groupRows(rows []modelRow) []groupedModelRow {
	byProvider := make(map[string]*groupedModelRow)
	order := make([]string, 0)

	for _, row := range rows {
		g, ok := byProvider[row.Provider]
		if !ok {
			g = &groupedModelRow{Provider: row.Provider, Enabled: row.Enabled, Models: make([]string, 0)}
			byProvider[row.Provider] = g
			order = append(order, row.Provider)
		}
		g.Models = append(g.Models, row.Model)
	}

	out := make([]groupedModelRow, 0, len(order))
	for _, provider := range order {
		out = append(out, *byProvider[provider])
	}
	return out
}

func fetchAvailableModels(name string, cfg providers.Config, timeoutSec int) []string {
	return fetchAvailableModelsContext(context.Background(), name, cfg, timeoutSec)
}

func fetchAvailableModelsContext(ctx context.Context, name string, cfg providers.Config, timeoutSec int) []string {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	if cfg.GetProviderType() == "gemini" {
		return fetchGeminiModelsContext(ctx, name, cfg, timeoutSec)
	}
	return fetchOpenAICompatibleModelsContext(ctx, name, cfg, timeoutSec)
}

func fetchOpenAICompatibleModels(name string, cfg providers.Config, timeoutSec int) []string {
	return fetchOpenAICompatibleModelsContext(context.Background(), name, cfg, timeoutSec)
}

func fetchOpenAICompatibleModelsContext(ctx context.Context, name string, cfg providers.Config, timeoutSec int) []string {
	if strings.TrimSuffix(cfg.BaseURL, "/") == "" {
		return nil
	}
	spec := catalog.BuildOpenAIModelsRequestSpec(cfg.BaseURL, providers.GetAPIKey(name, cfg), cfg.ExtraHeaders)
	return fetchModelListContext(ctx, spec.Endpoint, spec.Headers, timeoutSec, catalog.DecodeOpenAICompatibleModelIDs)
}

func fetchGeminiModels(name string, cfg providers.Config, timeoutSec int) []string {
	return fetchGeminiModelsContext(context.Background(), name, cfg, timeoutSec)
}

func fetchGeminiModelsContext(ctx context.Context, name string, cfg providers.Config, timeoutSec int) []string {
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	if baseURL == "" {
		return nil
	}
	apiKey := providers.GetAPIKey(name, cfg)
	if apiKey == "" {
		return nil
	}

	url := baseURL + "/models?key=" + apiKey
	return fetchModelListContext(ctx, url, nil, timeoutSec, catalog.DecodeGeminiModelIDs)
}

type modelListDecoder func([]byte) ([]string, error)

// fetchModelListContext runs the command's best-effort model-list HTTP lifecycle.
// Protocol-specific callers retain endpoint, authentication, and decoding policy.
func fetchModelListContext(ctx context.Context, endpoint string, headers map[string]string, timeoutSec int, decode modelListDecoder) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MB cap on model list responses
	if err != nil {
		return nil
	}

	models, err := decode(body)
	if err != nil {
		return nil
	}

	out := make([]string, 0, len(models))
	out = append(out, models...)
	sort.Strings(out)
	return out
}
