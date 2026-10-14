package cmd

import (
	"io"
	"sort"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

type pendingOMLXScores struct {
	path     string
	snapshot evals.OMLXScoreSnapshot
}

// prepareOMLXScores builds a complete in-memory category snapshot. Its caller
// owns the terminal publish step so a failed validation, encoding, or report
// write cannot replace the last known-good model-list state.
func prepareOMLXScores(report *evals.Report, discovery evals.OMLXDiscovery, liveInventory bool) (pendingOMLXScores, error) {
	path, err := catalog.Path()
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
	config, ok := cfg.Providers[omlx1]
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
		pc = cat.Providers[omlx1]
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
		input.Observations = omlxOperationalObservations(entry, costMap, key)
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
