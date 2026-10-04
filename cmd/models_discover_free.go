package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

const defaultDiscoverFreePrompt = "Write three short sentences about the sky. Output only the sentences."

func (cliOpts *invocationOptions) runModelsDiscoverFree(cmd *commandIO, args []string) error {
	out := cmd.OutOrStdout()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}
	catPath, err := catalog.CatalogPath()
	if err != nil {
		return err
	}

	refreshProviders := providerArgs(cliOpts.discoverFreeProviders)
	results, err := catalog.Refresh(cmd.Context(), refreshProviders, cfg.Providers, catPath)
	if err != nil {
		return fmt.Errorf("refresh catalog: %w", err)
	}
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(out, "%-14s refresh failed: %s\n", r.Provider, r.Err)
		}
	}

	cat, err := catalog.Load(catPath)
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	costMap, err := costs.LoadAll()
	if err != nil {
		return fmt.Errorf("load model costs: %w", err)
	}
	selected, err := catalog.ResolveModels(cat, cfg.Providers, costMap, catalog.SelectorOptions{
		Selector:           "free",
		ProviderFilter:     cliOpts.discoverFreeProviders,
		IncludeQuarantine:  cliOpts.discoverFreeIncludeQuarantine,
		Blocklist:          providers.NewBlocklist(cfg.Blocklist),
		MaxOutputCost:      cfg.MaxOutputCost,
		IncludeUnknownCost: true,
	})
	if err != nil {
		return err
	}

	targets := make([]pingTarget, 0, len(selected))
	for _, target := range selected {
		targets = append(targets, pingTarget{
			Name:       target.Provider,
			Config:     target.Config,
			CostStatus: target.CostStatus,
			InputCost:  target.InputPer1M,
			OutputCost: target.OutputPer1M,
		})
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
	now := time.Now().UTC()
	for _, result := range resultsPing {
		printResult(out, result)
	}
	if err := catalog.Update(cmd.Context(), catPath, func(cat *catalog.Catalog) error {
		for _, result := range resultsPing {
			catalog.RecordPingWithMetrics(cat, result.Provider, result.Model, result.Success, result.Latency, result.TTFB, result.Generation, result.SpeedTokensPS, result.TokensIn, result.TokensOut, result.Error, now)
			if cliOpts.discoverFreePin && result.Success {
				if entry := findCatalogEntry(cat, result.Provider, result.Model); entry != nil {
					entry.Pinned = true
				}
			}
		}
		return nil
	}); err != nil {
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

// Scalar helpers retain their signatures with independent default options.
func runModelsDiscoverFree(cmd *commandIO, args []string) error {
	return defaultInvocationOptions().runModelsDiscoverFree(cmd, args)
}
