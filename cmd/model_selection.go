package cmd

import (
	"fmt"
	"io"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

// resolveTextChatSelection resolves catalog targets and excludes models that cannot
// serve plain text chat requests. Callers retain config, cost, and option policy.
func resolveTextChatSelection(errOut io.Writer, catalogPath string, cfgs map[string]providers.Config, costMap map[string]costs.ModelCost, opts catalog.SelectorOptions) ([]catalog.ModelTarget, error) {
	cat, err := catalog.Load(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("load catalog: %w", err)
	}
	selected, err := catalog.ResolveModels(cat, cfgs, costMap, opts)
	if err != nil {
		return nil, err
	}
	selected, skippedNonChat := filterTextChatTargets(errOut, cat, selected, func(target catalog.ModelTarget) (string, string) {
		return target.Provider, target.Model
	})
	writeTextChatSkippedSummary(errOut, skippedNonChat)
	if len(selected) == 0 {
		return nil, fmt.Errorf("no text chat-capable models match selector %q", opts.Selector)
	}
	return selected, nil
}
