package writingcampaign

import (
	"fmt"
	"strings"
)

func (model Model) validateRosterEntry(maximumOutputPer1M float64) error {
	if err := model.validateRosterPricing(maximumOutputPer1M); err != nil {
		return err
	}
	if model.MaxCompletionTokens < 1 {
		return fmt.Errorf("model %q has no completion-token limit", model.OpenRouterModelID)
	}
	if model.Disabled && strings.TrimSpace(model.DisabledReason) == "" {
		return fmt.Errorf("disabled model %q has no reason", model.OpenRouterModelID)
	}
	if !model.Disabled && strings.TrimSpace(model.DisabledReason) != "" {
		return fmt.Errorf("enabled model %q has a disabled reason", model.OpenRouterModelID)
	}
	return nil
}

func (model Model) validateRosterPricing(maximumOutputPer1M float64) error {
	if model.InputPer1M < 0 || model.OutputPer1M < 0 || model.OutputPer1M > maximumOutputPer1M {
		return fmt.Errorf("model %q violates pricing policy", model.OpenRouterModelID)
	}
	if model.DynamicRouting {
		if model.OpenRouterModelID != "openrouter/auto" || model.RoutingMaxInputPer1M <= 0 || model.RoutingMaxOutputPer1M <= 0 || model.RoutingMaxOutputPer1M > maximumOutputPer1M {
			return fmt.Errorf("model %q has invalid dynamic-routing price limits", model.OpenRouterModelID)
		}
	} else if model.RoutingMaxInputPer1M != 0 || model.RoutingMaxOutputPer1M != 0 {
		return fmt.Errorf("model %q has routing limits without dynamic routing", model.OpenRouterModelID)
	}
	return nil
}
