package catalog

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

const (
	// QuarantineDuration is how long a health failure quarantines a model.
	QuarantineDuration = 2 * time.Hour
	// SlowPingThreshold is the latency above which a backend is marked slow.
	SlowPingThreshold = 5 * time.Second
)

// CostStatus classifies a model's known pricing.
type CostStatus string

// Cost status values.
const (
	CostUnknown CostStatus = "unknown"
	CostFree    CostStatus = free1
	CostPaid    CostStatus = "paid"
)

// SelectorOptions controls model resolution filters and budget bounds.
type SelectorOptions struct {
	Selector           string
	ProviderFilter     string
	IncludeQuarantine  bool
	FreeOnly           bool
	IncludeUnknownCost bool
	MaxOutputCost      float64
	Now                time.Time
	Blocklist          providers.Blocklist
}

// ModelTarget is one resolved provider/model pair with cost and quarantine
// state.
type ModelTarget struct {
	Provider         string
	Model            string
	Config           providers.Config
	CostStatus       CostStatus
	InputPer1M       float64
	OutputPer1M      float64
	EstimatedInput   float64
	EstimatedOutput  float64
	QuarantinedUntil time.Time
}

// ResolveModels resolves enabled providers into costed, policy-filtered model
// targets ordered by provider priority.
func ResolveModels(cat *Catalog, cfgs map[string]providers.Config, costMap map[string]costs.ModelCost, opts SelectorOptions) ([]ModelTarget, error) {
	opts = withResolutionDefaults(opts)
	selectorRaw, selector := normalizeSelector(opts.Selector)
	allowedProviders := providerFilterSet(opts.ProviderFilter)

	targets := make([]ModelTarget, 0)
	for _, entry := range providers.FilterEnabledProviders(cfgs) {
		if !providerAllowed(entry.Name, allowedProviders) {
			continue
		}
		targets = append(targets, resolveProviderModels(cat, entry, modelSelector{raw: selectorRaw, normalized: selector}, costMap, opts)...)
	}

	sortModelTargets(targets)

	if len(targets) == 0 {
		return nil, fmt.Errorf("no models match selector %q", selector)
	}
	return targets, nil
}

// withResolutionDefaults fills the evaluation time when unset.
func withResolutionDefaults(opts SelectorOptions) SelectorOptions {
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	return opts
}

// normalizeSelector trims and lowercases the selector, defaulting to the
// provider-configured models.
func normalizeSelector(raw string) (selectorRaw, selector string) {
	selectorRaw = strings.TrimSpace(raw)
	selector = strings.ToLower(selectorRaw)
	if selector == "" {
		selector = configured1
		selectorRaw = selector
	}
	return selectorRaw, selector
}

// providerAllowed applies the comma-separated provider filter; an empty filter
// admits every provider.
func providerAllowed(name string, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[strings.ToLower(name)]
	return ok
}

type modelSelector struct {
	raw        string
	normalized string
}

// resolveProviderModels runs the candidate pipeline for one provider.
func resolveProviderModels(cat *Catalog, entry providers.ProviderEntry, selection modelSelector, costMap map[string]costs.ModelCost, opts SelectorOptions) []ModelTarget {
	selectorRaw, selector := selection.raw, selection.normalized
	targets := make([]ModelTarget, 0)
	for _, model := range candidateModels(cat, entry.Name, entry.Config, selectorRaw, selector) {
		ctx := matchContext{provider: entry.Name, model: model, costMap: costMap, now: opts.Now}
		ctx.entry = modelEntry(cat, entry.Name, model)
		if !candidateSelected(selector, ctx, opts) {
			continue
		}
		quote := modelCostStatusForEntry(costMap, entry.Name, model, ctx.entry)
		if !costAllowed(quote, opts) {
			continue
		}
		targets = append(targets, newModelTarget(entry, model, ctx.entry, quote))
	}
	return targets
}

// candidateSelected reports whether the blocklist, avoid, quarantine, and
// selector filters all permit the candidate.
func candidateSelected(selector string, ctx matchContext, opts SelectorOptions) bool {
	if opts.Blocklist.Blocked(ctx.provider, ctx.model) {
		return false
	}
	entry := ctx.entry
	if entry != nil {
		if entry.Avoid {
			return false
		}
		if !opts.IncludeQuarantine && entry.QuarantineUntil.After(ctx.now) {
			return false
		}
	}
	return matchesSelector(selector, ctx)
}

// newModelTarget materializes a resolved target with the per-model config.
func newModelTarget(entry providers.ProviderEntry, model string, catEntry *ModelEntry, quote costQuote) ModelTarget {
	cfg := entry.Config
	cfg.Model = model
	target := ModelTarget{
		Provider:    entry.Name,
		Model:       model,
		Config:      cfg,
		CostStatus:  quote.status,
		InputPer1M:  quote.inputPer1M,
		OutputPer1M: quote.outputPer1M,
	}
	if catEntry != nil {
		target.QuarantinedUntil = catEntry.QuarantineUntil
	}
	return target
}

// sortModelTargets orders targets by provider priority, then provider, then model.
func sortModelTargets(targets []ModelTarget) {
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Config.Priority != targets[j].Config.Priority {
			return targets[i].Config.Priority < targets[j].Config.Priority
		}
		if targets[i].Provider != targets[j].Provider {
			return targets[i].Provider < targets[j].Provider
		}
		return targets[i].Model < targets[j].Model
	})
}
