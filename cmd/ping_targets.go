package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

func loadCatalogForPingFilter() (*catalog.Catalog, error) {
	catPath, err := catalog.CatalogPath()
	if err != nil {
		return nil, err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return nil, fmt.Errorf("load catalog: %w", err)
	}
	return cat, nil
}

func buildPingTargets(configs map[string]providers.Config, costMap map[string]costs.ModelCost, bl providers.Blocklist, maxOutputCost float64, includeUnknown bool, cat *catalog.Catalog) []pingTarget {
	return buildPingTargetsWithWriter(io.Discard, configs, costMap, bl, maxOutputCost, includeUnknown, cat)
}

func buildPingTargetsWithWriter(errOut io.Writer, configs map[string]providers.Config, costMap map[string]costs.ModelCost, bl providers.Blocklist, maxOutputCost float64, includeUnknown bool, cat *catalog.Catalog) []pingTarget {
	enabled := providers.FilterEnabledProviders(configs)
	targets := make([]pingTarget, 0, len(enabled))
	skippedNonChat := 0

	for _, entry := range enabled {
		models := modelVariants(entry.Config)
		for _, model := range models {
			if bl.Blocked(entry.Name, model) {
				continue
			}
			if ok, reason := catalog.TextChatCapability(entry.Name, model, modelEntryForTarget(cat, entry.Name, model)); !ok {
				skippedNonChat++
				fmt.Fprintf(errOut, "Chat filter: skipped %s/%s (%s)\n", entry.Name, model, reason)
				continue
			}
			cfg := entry.Config
			cfg.Model = model
			status, input, output := catalog.CostForModel(costMap, entry.Name, model)
			if !catalog.CostWithinCap(status, output, maxOutputCost, includeUnknown) {
				continue
			}
			targets = append(targets, pingTarget{
				Name:       entry.Name,
				Config:     cfg,
				CostStatus: status,
				InputCost:  input,
				OutputCost: output,
			})
		}
	}

	// Stable ordering: provider priority, then provider name, then model name.
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Config.Priority != targets[j].Config.Priority {
			return targets[i].Config.Priority < targets[j].Config.Priority
		}
		if targets[i].Name != targets[j].Name {
			return targets[i].Name < targets[j].Name
		}
		return targets[i].Config.Model < targets[j].Config.Model
	})

	// Filter by output cost cap if set.
	filteredCount := 0
	unknownCount := 0
	if pingMaxOutputCost > 0 {
		if len(costMap) > 0 {
			kept := targets[:0]
			for _, t := range targets {
				if t.CostStatus == catalog.CostUnknown {
					unknownCount++
					if pingIncludeUnknownCost {
						kept = append(kept, t)
					}
					continue
				}
				if t.CostStatus == catalog.CostPaid && t.OutputCost > pingMaxOutputCost {
					filteredCount++
					continue
				}
				kept = append(kept, t)
			}
			targets = kept
		}
	}
	if filteredCount > 0 {
		fmt.Fprintf(errOut, "Cost filter: skipped %d, unknown pricing %d (cap $%.2f/1M output)\n",
			filteredCount, unknownCount, pingMaxOutputCost)
	}
	if skippedNonChat > 0 {
		fmt.Fprintf(errOut, "Chat filter: skipped %d non-text chat target(s)\n", skippedNonChat)
	}

	return targets
}

func modelEntryForTarget(cat *catalog.Catalog, providerName, modelID string) *catalog.ModelEntry {
	if cat == nil {
		return nil
	}
	pc := cat.Providers[providerName]
	if pc == nil {
		return nil
	}
	return pc.Models[modelID]
}

func filterByProvider(targets []pingTarget, providerFilter string) []pingTarget {
	if providerFilter == "" {
		return targets
	}
	allowed := make(map[string]struct{})
	for _, provider := range normalizeCSVTokens(providerFilter) {
		allowed[provider] = struct{}{}
	}
	filtered := make([]pingTarget, 0, len(targets))
	for _, t := range targets {
		if _, ok := allowed[strings.ToLower(t.Name)]; ok {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func modelVariants(cfg providers.Config) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0, len(cfg.Models)+1)

	add := func(m string) {
		trimmed := strings.TrimSpace(m)
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		models = append(models, trimmed)
	}

	add(cfg.Model)
	for _, m := range cfg.Models {
		add(m)
	}

	if len(models) == 0 {
		models = append(models, cfg.Model)
	}

	return models
}
