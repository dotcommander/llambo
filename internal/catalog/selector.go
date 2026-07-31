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
	QuarantineDuration = 2 * time.Hour
	SlowPingThreshold  = 5 * time.Second
)

type CostStatus string

const (
	CostUnknown CostStatus = "unknown"
	CostFree    CostStatus = "free"
	CostPaid    CostStatus = "paid"
)

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

func ResolveModels(cat *Catalog, cfgs map[string]providers.Config, costMap map[string]costs.ModelCost, opts SelectorOptions) ([]ModelTarget, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	selectorRaw := strings.TrimSpace(opts.Selector)
	selector := strings.ToLower(selectorRaw)
	if selector == "" {
		selector = "configured"
		selectorRaw = selector
	}

	allowedProviders := providerFilterSet(opts.ProviderFilter)
	targets := make([]ModelTarget, 0)
	for _, entry := range providers.FilterEnabledProviders(cfgs) {
		if len(allowedProviders) > 0 {
			if _, ok := allowedProviders[strings.ToLower(entry.Name)]; !ok {
				continue
			}
		}

		for _, model := range candidateModels(cat, entry.Name, entry.Config, selectorRaw, selector) {
			if opts.Blocklist.Blocked(entry.Name, model) {
				continue
			}
			catEntry := modelEntry(cat, entry.Name, model)
			if catEntry != nil && catEntry.Avoid {
				continue
			}
			if catEntry != nil && !opts.IncludeQuarantine && catEntry.QuarantineUntil.After(opts.Now) {
				continue
			}
			if !matchesSelector(selector, entry.Name, model, catEntry, costMap, opts.Now) {
				continue
			}

			status, input, output := modelCostStatusForEntry(costMap, entry.Name, model, catEntry)
			if !costAllowed(status, output, opts) {
				continue
			}

			cfg := entry.Config
			cfg.Model = model
			target := ModelTarget{
				Provider:    entry.Name,
				Model:       model,
				Config:      cfg,
				CostStatus:  status,
				InputPer1M:  input,
				OutputPer1M: output,
			}
			if catEntry != nil {
				target.QuarantinedUntil = catEntry.QuarantineUntil
			}
			targets = append(targets, target)
		}
	}

	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Config.Priority != targets[j].Config.Priority {
			return targets[i].Config.Priority < targets[j].Config.Priority
		}
		if targets[i].Provider != targets[j].Provider {
			return targets[i].Provider < targets[j].Provider
		}
		return targets[i].Model < targets[j].Model
	})

	if len(targets) == 0 {
		return nil, fmt.Errorf("no models match selector %q", selector)
	}
	return targets, nil
}
