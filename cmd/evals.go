package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

var (
	evalsRefresh              bool
	evalsRefreshOfficialCards bool
	evalsFormat               string
	evalsOutput               string
	evalsLimit                int
	evalsPartial              bool
	evalsRankBy               string
	evalsOffline              bool
	evalsProjections          string
	evalsValidationReceipts   string
	evalsMinScore             float64
	evalsMaxOutputPrice       float64
	evalsOMLXURL              string
	evalsNoOMLX               bool
	evalsCacheDir             string
	omlxDiscoveryHTTPClient   = &http.Client{Timeout: 5 * time.Second}
	fetchEvalsReport          = evals.Fetch
	prepareEvalsOMLXScores    = prepareOMLXScores
)

func runEvals(cmd *commandIO, _ []string) error {
	if evalsOffline && evalsRefresh {
		return fmt.Errorf("--offline and --refresh cannot be used together")
	}
	if evalsRefresh && evalsRefreshOfficialCards {
		return fmt.Errorf("--refresh and --refresh-official-model-cards cannot be used together")
	}
	if evalsOffline && evalsRefreshOfficialCards {
		return fmt.Errorf("--offline and --refresh-official-model-cards cannot be used together")
	}
	if math.IsNaN(evalsMinScore) || math.IsInf(evalsMinScore, 0) || evalsMinScore < -1 || evalsMinScore > 100 {
		return fmt.Errorf("--min-score must be between -1 and 100")
	}
	if evalsMinScore >= 0 && !evals.IsCapabilityCategory(strings.ToLower(strings.TrimSpace(evalsRankBy))) {
		return fmt.Errorf("--min-score requires a capability --rank-by category")
	}
	if math.IsNaN(evalsMaxOutputPrice) || math.IsInf(evalsMaxOutputPrice, 0) || evalsMaxOutputPrice < -1 {
		return fmt.Errorf("--max-output-price must be -1 or greater")
	}
	cacheDir := strings.TrimSpace(evalsCacheDir)
	if cacheDir == "" {
		cacheRoot, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("resolve user cache directory: %w", err)
		}
		cacheDir = filepath.Join(cacheRoot, "llambo", "evals")
	}
	result, err := fetchEvalsReport(cmd.Context(), evalFetchOptions(cacheDir))
	if err != nil {
		return err
	}
	report, err := evals.BuildReportWithProjectionFile(result, strings.ToLower(strings.TrimSpace(evalsRankBy)), strings.TrimSpace(evalsProjections))
	if err != nil {
		return err
	}
	if strings.TrimSpace(evalsValidationReceipts) != "" {
		if err := evals.ApplyValidationReceipts(&report, strings.TrimSpace(evalsValidationReceipts)); err != nil {
			return err
		}
	}
	// Live OMLX discovery is an explicit inventory boundary, never an
	// evaluation. Ordinary cache-only runs keep the configured/snapshot set.
	discovery, liveInventory, err := applyLiveOMLXDiscovery(cmd, &report)
	if err != nil {
		return err
	}
	pending, err := prepareEvalsOMLXScores(&report, discovery, liveInventory)
	if err != nil {
		return err
	}
	evals.ApplyCategoryEligibility(&report, strings.ToLower(strings.TrimSpace(evalsRankBy)), evalsMinScore, evalsMaxOutputPrice)
	data, err := encodeEvalsReport(report, evalsFormat, evalsLimit)
	if err != nil {
		return err
	}
	if evalsOutput == "" {
		_, err = cmd.OutOrStdout().Write(data)
		if err != nil {
			return err
		}
		return publishOMLXScores(pending)
	}
	return writeEvalsThenPublish(cmd.ErrOrStderr(), evalsOutput, data, pending)
}

type pendingOMLXScores struct {
	path     string
	snapshot evals.OMLXScoreSnapshot
}

// prepareOMLXScores builds a complete in-memory category snapshot. Its caller
// owns the terminal publish step so a failed validation, encoding, or report
// write cannot replace the last known-good model-list state.
func prepareOMLXScores(report *evals.Report, discovery evals.OMLXDiscovery, liveInventory bool) (pendingOMLXScores, error) {
	path, err := catalog.CatalogPath()
	if err != nil {
		return pendingOMLXScores{}, err
	}
	cat, err := catalog.Load(path)
	if err != nil {
		return pendingOMLXScores{}, err
	}
	costMap, err := costs.LoadAll()
	if err != nil {
		return pendingOMLXScores{}, err
	}
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return pendingOMLXScores{}, err
	}
	snapshotPath, err := evals.OMLXScoreSnapshotPath()
	if err != nil {
		return pendingOMLXScores{}, err
	}
	previous, err := evals.LoadOMLXScoreSnapshot(snapshotPath)
	if err != nil {
		return pendingOMLXScores{}, err
	}
	currentIDs, endpoint := selectedOMLXInventory(cfg, previous, discovery, liveInventory)
	inputs := omlxScoreInputs(report, cat, costMap, currentIDs)
	evals.ApplyOMLXCategoryScores(report, inputs)
	return pendingOMLXScores{path: snapshotPath, snapshot: evals.NewOMLXScoreSnapshot(*report, currentIDs, endpoint, time.Now().UTC())}, nil
}

func publishOMLXScores(pending pendingOMLXScores) error {
	return evals.SaveOMLXScoreSnapshot(pending.path, pending.snapshot)
}

func writeEvalsThenPublish(errOut io.Writer, output string, data []byte, pending pendingOMLXScores) error {
	if err := writeEvalsReportTo(errOut, output, data); err != nil {
		return err
	}
	return publishOMLXScores(pending)
}

func omlxConfiguredModelIDs(cfg *providers.GlobalConfig) []string {
	if cfg == nil {
		return nil
	}
	config, ok := cfg.Providers["omlx"]
	if !ok {
		return nil
	}
	return modelVariants(config)
}

func selectedOMLXInventory(cfg *providers.GlobalConfig, snapshot *evals.OMLXScoreSnapshot, discovery evals.OMLXDiscovery, live bool) ([]string, string) {
	if live {
		return sortedUniqueStrings(discovery.Models), discovery.Endpoint
	}
	ids := omlxConfiguredModelIDs(cfg)
	endpoint := ""
	if snapshot != nil {
		ids = append(ids, snapshot.Inventory...)
		endpoint = snapshot.Endpoint
	}
	return sortedUniqueStrings(ids), endpoint
}

func omlxScoreInputs(report *evals.Report, cat *catalog.Catalog, costMap map[string]costs.ModelCost, currentIDs []string) []evals.OMLXScoreInput {
	if cat == nil {
		return nil
	}
	var pc *catalog.ProviderCatalog
	if cat.Providers != nil {
		pc = cat.Providers["omlx"]
	}
	byKey := make(map[string]evals.ReportModel, len(report.Models))
	for _, row := range report.Models {
		byKey[row.Key] = row
	}
	ordered := sortedUniqueStrings(currentIDs)
	inputs := make([]evals.OMLXScoreInput, 0, len(ordered))
	for _, key := range ordered {
		var entry *catalog.ModelEntry
		if pc != nil {
			entry = pc.Models[key]
		}
		row, found := byKey[key]
		input := evals.OMLXScoreInput{Key: key}
		if found {
			input.Projection, input.Capabilities = row.Projection, row.LlamboScores
		}
		if entry != nil {
			for source, benchmark := range entry.Benchmarks {
				if benchmark.SpeedTokensPerSecond > 0 {
					input.Observations = append(input.Observations, evals.OMLXOperationalObservation{Metric: "speed", Value: benchmark.SpeedTokensPerSecond, Source: source, MeasuredAt: benchmark.UpdatedAt, Benchmark: true})
				}
				if benchmark.LatencyMS > 0 {
					input.Observations = append(input.Observations, evals.OMLXOperationalObservation{Metric: "latency", Value: float64(benchmark.LatencyMS), Source: source, MeasuredAt: benchmark.UpdatedAt, Benchmark: true})
				}
			}
			if entry.LastPing.Success {
				if entry.LastPing.SpeedTokensPerSecond > 0 {
					input.Observations = append(input.Observations, evals.OMLXOperationalObservation{Metric: "speed", Value: entry.LastPing.SpeedTokensPerSecond, Source: "ping", MeasuredAt: entry.LastPing.CheckedAt, Successful: true})
				}
				if entry.LastPing.LatencyMS > 0 {
					input.Observations = append(input.Observations, evals.OMLXOperationalObservation{Metric: "latency", Value: float64(entry.LastPing.LatencyMS), Source: "ping", MeasuredAt: entry.LastPing.CheckedAt, Successful: true})
				}
			}
			status, _, output := catalog.CostForEntry(costMap, "omlx", key, entry)
			if status != catalog.CostUnknown {
				input.Observations = append(input.Observations, evals.OMLXOperationalObservation{Metric: "output-price", Value: output, Source: "catalog-pricing", TimestampUnknown: true, Benchmark: true})
			}
		}
		inputs = append(inputs, input)
	}
	return inputs
}

func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func applyLiveOMLXDiscovery(cmd *commandIO, report *evals.Report) (evals.OMLXDiscovery, bool, error) {
	if evalsOffline || evalsNoOMLX {
		return evals.OMLXDiscovery{}, false, nil
	}
	discovery, err := evals.DiscoverOMLXModels(cmd.Context(), evalsOMLXURL, os.Getenv("OMLX_API_KEY"), omlxDiscoveryHTTPClient)
	if err != nil {
		return discovery, false, fmt.Errorf("discover live OMLX inventory: %w", err)
	}
	evals.FilterProjectedRowsToLiveOMLX(report, discovery, nil)
	return discovery, true, nil
}

func validateOMLXURL(value string) error {
	if err := evals.ValidateOMLXLoopbackBaseURL(value); err != nil {
		return fmt.Errorf("invalid --omlx-url: %w", err)
	}
	return nil
}

func evalFetchOptions(cacheDir string) evals.Options {
	return evals.Options{
		CacheDir:             cacheDir,
		Refresh:              evalsRefresh,
		RefreshOfficialCards: evalsRefreshOfficialCards,
		Offline:              !evalsRefresh && !evalsRefreshOfficialCards,
		AllowPartial:         evalsPartial,
		IngestLLMBenchmarks:  evalsRefresh,
		AAAPIKey:             os.Getenv("AA_API_KEY"),
		LLMStatsAPIKey:       os.Getenv("LLM_STATS_KEY"),
	}
}

func encodeEvalsReport(report evals.Report, format string, limit int) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown", "md":
		return []byte(evals.RenderMarkdown(report, limit)), nil
	case "html":
		return []byte(evals.RenderHTML(report, limit)), nil
	case "json":
		limited := limitEvalsModels(report.Models, limit)
		report.Models = report.Models[:0]
		report.ProjectedModels = nil
		for _, model := range limited {
			if model.Projection == nil {
				report.Models = append(report.Models, model)
			} else {
				report.ProjectedModels = append(report.ProjectedModels, model)
			}
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported --format %q (supported: markdown, json, html)", format)
	}
}

func limitEvalsModels(models []evals.ReportModel, limit int) []evals.ReportModel {
	if limit <= 0 {
		return models
	}
	limited := make([]evals.ReportModel, 0, min(len(models), limit))
	canonical := 0
	for _, model := range models {
		if model.Projection == nil {
			if canonical >= limit {
				continue
			}
			canonical++
		}
		limited = append(limited, model)
	}
	return limited
}

func writeEvalsReport(path string, data []byte) error {
	return writeEvalsReportTo(os.Stderr, path, data)
}

func writeEvalsReportTo(errOut io.Writer, path string, data []byte) error {
	if err := writeAtomicOutput(path, data); err != nil {
		return err
	}
	fmt.Fprintf(errOut, "Evaluation report written to %s\n", path)
	return nil
}

func writeAtomicOutput(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}
