package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/providers"
)

func sortedProviderNames(configs map[string]providers.Config) []string {
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedStringSliceMapKeys(values map[string][]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func enabledProviderConfigs(configs map[string]providers.Config) map[string]providers.Config {
	entries := providers.FilterEnabledProviders(configs)
	enabled := make(map[string]providers.Config, len(entries))
	for _, entry := range entries {
		enabled[entry.Name] = entry.Config
	}
	return enabled
}

func routingProviderConfigs(configs map[string]providers.Config, routing providers.RoutingConfig) (map[string]providers.Config, error) {
	enabled := enabledProviderConfigs(configs)
	if strings.TrimSpace(strings.ToLower(routing.CatalogModels)) != "pinned" {
		return enabled, nil
	}

	catalogPath, err := catalog.CatalogPath()
	if err != nil {
		return nil, err
	}
	cat, err := catalog.Load(catalogPath)
	if err != nil {
		return nil, err
	}
	pinned := catalog.PinnedModels(cat, configs)
	if len(pinned) == 0 {
		return nil, fmt.Errorf("routing.catalog_models is pinned but no pinned catalog models are available (run: llambo providers refresh; then: llambo models catalog pin <provider> <model>)")
	}

	expanded := make(map[string]providers.Config)
	for _, providerName := range sortedStringSliceMapKeys(pinned) {
		base, ok := enabled[providerName]
		if !ok {
			continue
		}
		for _, model := range pinned[providerName] {
			cfg := base
			cfg.Model = model
			applyCatalogModelEvidence(&cfg, cat.Providers[providerName].Models[model])
			expanded[catalogBackendName(providerName, model)] = cfg
		}
	}
	if len(expanded) == 0 {
		return nil, fmt.Errorf("routing.catalog_models is pinned but no pinned catalog models match enabled providers")
	}
	return expanded, nil
}

func catalogBackendName(providerName, model string) string {
	return providerName + ":" + model
}

func applyCatalogModelEvidence(cfg *providers.Config, entry *catalog.ModelEntry) {
	if entry == nil {
		return
	}
	applyCatalogModelMetadata(cfg, entry)
	task, evidence, ok := catalog.BestQualityEvidence(entry)
	if !ok {
		return
	}
	if evidence.Score > cfg.Quality {
		cfg.Quality = evidence.Score
	}
	if task != "" && !hasCapability(cfg.Capabilities, task) {
		cfg.Capabilities = append(cfg.Capabilities, task)
		sort.Strings(cfg.Capabilities)
	}
}

func applyCatalogModelMetadata(cfg *providers.Config, entry *catalog.ModelEntry) {
	status, inputCost, outputCost := catalog.CostForEntry(nil, "", "", entry)
	if status != catalog.CostUnknown {
		if cfg.InputCostPM == 0 {
			cfg.InputCostPM = inputCost
		}
		if cfg.OutputCostPM == 0 {
			cfg.OutputCostPM = outputCost
		}
	}

	addCatalogCapability(cfg, "long_context", metadataContextLength(entry) >= 128_000)
	addCatalogCapability(cfg, "tools", metadataSupportsAny(entry, "tools", "tool_choice"))
	addCatalogCapability(cfg, "structured_outputs", metadataSupportsAny(entry, "structured_outputs", "response_format"))
	addCatalogCapability(cfg, "reasoning", len(entry.Metadata.Reasoning) > 0 || metadataSupportsAny(entry, "reasoning", "include_reasoning", "reasoning_effort"))
}

func addCatalogCapability(cfg *providers.Config, capability string, enabled bool) {
	if !enabled || hasCapability(cfg.Capabilities, capability) {
		return
	}
	cfg.Capabilities = append(cfg.Capabilities, capability)
	sort.Strings(cfg.Capabilities)
}

func metadataContextLength(entry *catalog.ModelEntry) int {
	if entry.Metadata.TopProvider.ContextLength > 0 {
		return entry.Metadata.TopProvider.ContextLength
	}
	return entry.Metadata.ContextLength
}

func metadataSupportsAny(entry *catalog.ModelEntry, params ...string) bool {
	if len(entry.Metadata.SupportedParameters) == 0 {
		return false
	}
	wanted := make(map[string]struct{}, len(params))
	for _, param := range params {
		wanted[strings.ToLower(strings.TrimSpace(param))] = struct{}{}
	}
	for _, param := range entry.Metadata.SupportedParameters {
		if _, ok := wanted[strings.ToLower(strings.TrimSpace(param))]; ok {
			return true
		}
	}
	return false
}

func hasCapability(capabilities []string, capability string) bool {
	for _, existing := range capabilities {
		if strings.EqualFold(existing, capability) {
			return true
		}
	}
	return false
}
