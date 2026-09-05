package catalog

import (
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/costs"
)

// categoryPolicy keeps a category's candidate scope and matching rule together.
// configuredOnly deliberately applies only to the exact top-level selector spelling.
type categoryPolicy struct {
	configuredOnly bool
	matches        func(providerName, modelID string, m *ModelEntry, costMap map[string]costs.ModelCost, now time.Time) bool
}

func categoryPolicyForSelector(selector string) (categoryPolicy, bool) {
	category, ok := strings.CutPrefix(selector, "category:")
	if !ok {
		return categoryPolicy{}, false
	}
	return categoryPolicyFor(category), true
}

func categoryPolicyFor(category string) categoryPolicy {
	normalized := normalizeTag(category)
	policy := categoryPolicy{
		matches: func(_, _ string, m *ModelEntry, _ map[string]costs.ModelCost, _ time.Time) bool {
			return HasTag(m, normalized)
		},
	}

	switch normalized {
	case "free":
		policy.matches = func(providerName, modelID string, m *ModelEntry, costMap map[string]costs.ModelCost, _ time.Time) bool {
			status, _, _ := modelCostStatusForEntry(costMap, providerName, modelID, m)
			return status == CostFree
		}
	case "healthy":
		policy.matches = func(_, _ string, m *ModelEntry, _ map[string]costs.ModelCost, now time.Time) bool {
			return m != nil && m.LastPing.Success && !m.QuarantineUntil.After(now)
		}
	case "speed":
		policy.matches = func(_, _ string, m *ModelEntry, _ map[string]costs.ModelCost, now time.Time) bool {
			return m != nil && m.LastPing.Success && m.LastPing.LatencyMS > 0 && time.Duration(m.LastPing.LatencyMS)*time.Millisecond <= SlowPingThreshold && !m.QuarantineUntil.After(now)
		}
	case "missing_speed", "missing-speed":
		// Preserve the historical whitespace asymmetry: category:missing_speed
		// uses configured candidates, while category: missing_speed does not.
		policy.configuredOnly = category == normalized
		policy.matches = func(_, _ string, m *ModelEntry, _ map[string]costs.ModelCost, _ time.Time) bool {
			return !hasSpeedMeasurement(m)
		}
	case "long_context":
		policy.matches = func(_, _ string, m *ModelEntry, _ map[string]costs.ModelCost, _ time.Time) bool {
			return HasTag(m, normalized) || metadataContextLength(m) >= 128_000
		}
	case "tools":
		policy.matches = func(_, _ string, m *ModelEntry, _ map[string]costs.ModelCost, _ time.Time) bool {
			return HasTag(m, normalized) || metadataSupportsAny(m, "tools", "tool_choice")
		}
	case "structured_outputs":
		policy.matches = func(_, _ string, m *ModelEntry, _ map[string]costs.ModelCost, _ time.Time) bool {
			return HasTag(m, normalized) || metadataSupportsAny(m, "structured_outputs", "response_format")
		}
	case "reasoning":
		policy.matches = func(_, _ string, m *ModelEntry, _ map[string]costs.ModelCost, _ time.Time) bool {
			return HasTag(m, normalized) || metadataHasReasoning(m) || metadataSupportsAny(m, "reasoning", "include_reasoning", "reasoning_effort")
		}
	}

	return policy
}
