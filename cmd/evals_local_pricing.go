package cmd

import (
	"fmt"
	"math"
	"strings"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

// authoritativeLocalPricing reads the persisted pricing authority used by
// normal model selection. Explicit CSV values win; configured non-zero prices
// are a compatible fallback for provider entries that predate the CSV.
func authoritativeLocalPricing(model string) (evals.LocalPricing, bool, error) {
	providerName, modelID, ok := strings.Cut(strings.TrimSpace(model), "/")
	if !ok || providerName == "" || modelID == "" {
		return evals.LocalPricing{}, false, fmt.Errorf("model must be exact provider/model")
	}
	costMap, err := costs.LoadAll()
	if err != nil {
		return evals.LocalPricing{}, false, fmt.Errorf("load configured model pricing: %w", err)
	}
	if cost, ok := costMap[costs.Key(providerName, modelID)]; ok && cost.InputExplicit && cost.OutputExplicit {
		return evals.LocalPricing{InputPer1M: cost.InputPer1M, OutputPer1M: cost.OutputPer1M, Known: true}, true, nil
	}
	config, err := providers.LoadRawGlobalConfig()
	if err != nil {
		// A local, explicitly-zero OMLX run may use an isolated config home. Do
		// not turn absent configuration into an invented hosted price.
		return evals.LocalPricing{}, false, nil
	}
	cfg, ok := config.Providers[providerName]
	if !ok || (cfg.InputCostPM == 0 && cfg.OutputCostPM == 0) {
		return evals.LocalPricing{}, false, nil
	}
	return evals.LocalPricing{InputPer1M: cfg.InputCostPM, OutputPer1M: cfg.OutputCostPM, Known: true}, true, nil
}

func resolveLocalExecutionPricing(model string, caller evals.LocalPricing) (evals.LocalPricing, error) {
	if err := caller.Validate(); err != nil {
		return evals.LocalPricing{}, err
	}
	authoritative, found, err := authoritativeLocalPricing(model)
	if err != nil {
		return evals.LocalPricing{}, err
	}
	if found {
		if math.Abs(authoritative.InputPer1M-caller.InputPer1M) > 1e-12 || math.Abs(authoritative.OutputPer1M-caller.OutputPer1M) > 1e-12 {
			return evals.LocalPricing{}, fmt.Errorf("caller pricing disagrees with configured authoritative price for %s", model)
		}
		return authoritative, nil
	}
	if strings.HasPrefix(model, "omlx/") && caller.InputPer1M == 0 && caller.OutputPer1M == 0 {
		// The caller has explicitly declared the local endpoint free. This is a
		// known zero price, not a fallback for an unavailable hosted price.
		return caller, nil
	}
	return evals.LocalPricing{}, fmt.Errorf("no authoritative configured pricing for %s; explicit zero is allowed only for local omlx models", model)
}
