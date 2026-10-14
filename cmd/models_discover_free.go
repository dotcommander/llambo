package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

const defaultDiscoverFreePrompt = "Write three short sentences about the sky. Output only the sentences."

func (cliOpts *invocationOptions) runModelsDiscoverFree(cmd *commandIO) error {
	out := cmd.OutOrStdout()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}
	catPath, err := catalog.Path()
	if err != nil {
		return err
	}

	targets, err := cliOpts.refreshAndResolveFreeTargets(out, cmd.Context(), cfg, catPath)
	if err != nil {
		return err
	}
	timeout := time.Duration(cliOpts.discoverFreeTimeout) * time.Second
	resultsPing := runOrderedProviderGroups(targets, func(target pingTarget) string { return target.Name }, func(_ int, target pingTarget) PingResult {
		result := pingProviderContext(cmd.Context(), target.Name, target.Config, defaultDiscoverFreePrompt, timeout)
		result.CostStatus = string(target.CostStatus)
		result.InputCostPer1M = target.InputCost
		result.OutputCostPer1M = target.OutputCost
		return result
	})

	if err := cmd.Context().Err(); err != nil {
		return err
	}
	for _, result := range resultsPing {
		printResult(out, result)
	}
	return cliOpts.recordDiscoverFreeHealth(cmd.Context(), catPath, resultsPing)
}

// refreshAndResolveFreeTargets refreshes catalog inventory, reports per-provider
// refresh failures, and resolves the zero-price selector into ping targets.
func (cliOpts *invocationOptions) refreshAndResolveFreeTargets(out io.Writer, ctx context.Context, cfg *providers.GlobalConfig, catPath string) ([]pingTarget, error) {
	results, err := catalog.Refresh(ctx, providerArgs(cliOpts.discoverFreeProviders), cfg.Providers, catPath)
	if err != nil {
		return nil, fmt.Errorf("refresh catalog: %w", err)
	}
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(out, "%-14s refresh failed: %s\n", r.Provider, r.Err)
		}
	}

	cat, err := catalog.Load(catPath)
	if err != nil {
		return nil, fmt.Errorf("load catalog: %w", err)
	}
	costMap, err := costs.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("load model costs: %w", err)
	}
	selected, err := catalog.ResolveModels(cat, cfg.Providers, costMap, catalog.SelectorOptions{
		Selector:           free1,
		ProviderFilter:     cliOpts.discoverFreeProviders,
		IncludeQuarantine:  cliOpts.discoverFreeIncludeQuarantine,
		Blocklist:          providers.NewBlocklist(cfg.Blocklist),
		MaxOutputCost:      cfg.MaxOutputCost,
		IncludeUnknownCost: true,
	})
	if err != nil {
		return nil, err
	}
	return pingTargetsFromSelection(selected), nil
}

// recordDiscoverFreeHealth persists ping health and optional auto-pinning.
func (cliOpts *invocationOptions) recordDiscoverFreeHealth(ctx context.Context, catPath string, resultsPing []PingResult) error {
	now := time.Now().UTC()
	err := catalog.Update(ctx, catPath, func(cat *catalog.Catalog) error {
		for _, result := range resultsPing {
			catalog.RecordPingWithMetrics(cat, result.Provider, result.Model, pingHealthObservation(result, now))
			if cliOpts.discoverFreePin && result.Success {
				if entry := findCatalogEntry(cat, result.Provider, result.Model); entry != nil {
					entry.Pinned = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save catalog health: %w", err)
	}
	return nil
}

func providerArgs(csv string) []string {
	var out []string
	for _, token := range strings.Split(csv, ",") {
		token = strings.TrimSpace(token)
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

func findCatalogEntry(cat *catalog.Catalog, providerName, modelID string) *catalog.ModelEntry {
	if cat == nil || cat.Providers == nil {
		return nil
	}
	pc := cat.Providers[providerName]
	if pc == nil || pc.Models == nil {
		return nil
	}
	return pc.Models[modelID]
}
