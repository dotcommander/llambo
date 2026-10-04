package catalog

import (
	"sort"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

func candidateModels(cat *Catalog, providerName string, cfg providers.Config, selectorRaw, selector string) []string {
	if provider, model, ok := splitDirectSelector(selectorRaw); ok {
		if !strings.EqualFold(provider, providerName) {
			return nil
		}
		return directSelectorCandidateModels(cat, providerName, cfg, model)
	}
	if selector == "configured" {
		return configModels(cfg)
	}
	if policy, ok := categoryPolicyForSelector(selector); ok && policy.configuredOnly {
		return configModels(cfg)
	}
	pc := providerCatalog(cat, providerName)
	if pc == nil || len(pc.Models) == 0 {
		return configModels(cfg)
	}
	seen := make(map[string]bool, len(pc.Models))
	models := make([]string, 0, len(pc.Models))
	for _, id := range configModels(cfg) {
		if !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	for id := range pc.Models {
		if !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models
}

func matchesSelector(selector, providerName, modelID string, m *ModelEntry, costMap map[string]costs.ModelCost, now time.Time) bool {
	switch {
	case directSelectorMatches(selector, providerName, modelID):
		return true
	case selector == "configured" || selector == "all":
		return true
	case selector == "free":
		status, _, _ := modelCostStatusForEntry(costMap, providerName, modelID, m)
		return status == CostFree
	case selector == "pinned":
		return m != nil && m.Pinned
	case selector == "healthy":
		return m != nil && m.LastPing.Success && !m.QuarantineUntil.After(now)
	case strings.HasPrefix(selector, "tag:"):
		return HasTag(m, strings.TrimPrefix(selector, "tag:"))
	case strings.HasPrefix(selector, "category:"):
		return matchesCategory(strings.TrimPrefix(selector, "category:"), providerName, modelID, m, costMap, now)
	default:
		return false
	}
}

func splitDirectSelector(selector string) (string, string, bool) {
	provider, model, ok := strings.Cut(strings.TrimSpace(selector), "/")
	if !ok || strings.TrimSpace(provider) == "" || strings.TrimSpace(model) == "" {
		return "", "", false
	}
	return strings.TrimSpace(provider), strings.TrimSpace(model), true
}

func directSelectorCandidateModels(cat *Catalog, providerName string, cfg providers.Config, selectedModel string) []string {
	models := append([]string(nil), configModels(cfg)...)
	if pc := providerCatalog(cat, providerName); pc != nil {
		for model := range pc.Models {
			models = append(models, model)
		}
	}

	seen := make(map[string]struct{})
	for _, model := range models {
		if !strings.EqualFold(model, selectedModel) {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		return []string{model}
	}
	return []string{selectedModel}
}

func directSelectorMatches(selector, providerName, modelID string) bool {
	provider, model, ok := splitDirectSelector(selector)
	return ok && strings.EqualFold(provider, providerName) && strings.EqualFold(model, modelID)
}

func matchesCategory(category, providerName, modelID string, m *ModelEntry, costMap map[string]costs.ModelCost, now time.Time) bool {
	return categoryPolicyFor(category).matches(providerName, modelID, m, costMap, now)
}

func hasSpeedMeasurement(m *ModelEntry) bool {
	if m == nil {
		return false
	}
	if m.LastPing.SpeedTokensPerSecond > 0 {
		return true
	}
	for _, benchmark := range m.Benchmarks {
		if benchmark.SpeedTokensPerSecond > 0 {
			return true
		}
	}
	return false
}

func metadataContextLength(m *ModelEntry) int {
	if m == nil {
		return 0
	}
	if m.Metadata.TopProvider.ContextLength > 0 {
		return m.Metadata.TopProvider.ContextLength
	}
	return m.Metadata.ContextLength
}

func metadataSupportsAny(m *ModelEntry, params ...string) bool {
	if m == nil || len(m.Metadata.SupportedParameters) == 0 {
		return false
	}
	wanted := make(map[string]struct{}, len(params))
	for _, param := range params {
		wanted[normalizeTag(param)] = struct{}{}
	}
	for _, param := range m.Metadata.SupportedParameters {
		if _, ok := wanted[normalizeTag(param)]; ok {
			return true
		}
	}
	return false
}

func metadataHasReasoning(m *ModelEntry) bool {
	return m != nil && len(m.Metadata.Reasoning) > 0
}

func modelEntry(cat *Catalog, providerName, modelID string) *ModelEntry {
	pc := providerCatalog(cat, providerName)
	if pc == nil {
		return nil
	}
	return pc.Models[modelID]
}

func providerCatalog(cat *Catalog, providerName string) *ProviderCatalog {
	if cat == nil || cat.Providers == nil {
		return nil
	}
	return cat.Providers[providerName]
}

func configModels(cfg providers.Config) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(cfg.Models)+1)
	add := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	add(cfg.Model)
	for _, model := range cfg.Models {
		add(model)
	}
	return out
}

func providerFilterSet(csv string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, token := range strings.Split(csv, ",") {
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "" {
			out[token] = struct{}{}
		}
	}
	return out
}
