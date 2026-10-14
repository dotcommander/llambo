package catalog

import (
	"strings"
	"time"
)

// categoryPolicy keeps a category's candidate scope and matching rule together.
// configuredOnly deliberately applies only to the exact top-level selector spelling.
type categoryPolicy struct {
	configuredOnly bool
	matches        func(ctx matchContext) bool
}

func categoryPolicyForSelector(selector string) (categoryPolicy, bool) {
	category, ok := strings.CutPrefix(selector, "category:")
	if !ok {
		return categoryPolicy{}, false
	}
	return categoryPolicyFor(category), true
}

// categoryPolicyFor resolves the matching rule for one category; unknown
// categories fall back to normalized tag membership.
func categoryPolicyFor(category string) categoryPolicy {
	normalized := normalizeTag(category)
	policy := categoryPolicy{
		matches: func(ctx matchContext) bool {
			return HasTag(ctx.entry, normalized)
		},
	}

	switch normalized {
	case free1:
		policy.matches = matchFreeCategory
	case "healthy":
		policy.matches = matchHealthyCategory
	case "speed":
		policy.matches = matchSpeedCategory
	case "missing_speed", "missing-speed":
		// Preserve the historical whitespace asymmetry: category:missing_speed
		// uses configured candidates, while category: missing_speed does not.
		policy.configuredOnly = category == normalized
		policy.matches = matchMissingSpeedCategory
	case "long_context":
		policy.matches = matchLongContextCategory
	case "tools":
		policy.matches = matchToolsCategory
	case "structured_outputs":
		policy.matches = matchStructuredOutputsCategory
	case "reasoning":
		policy.matches = matchReasoningCategory
	}

	return policy
}

func matchFreeCategory(ctx matchContext) bool {
	return ctx.costStatus() == CostFree
}

func matchHealthyCategory(ctx matchContext) bool {
	return healthyPingSelection(ctx.entry, ctx.now)
}

// matchSpeedCategory admits successful, unquarantined pings at or below
// SlowPingThreshold.
func matchSpeedCategory(ctx matchContext) bool {
	entry := ctx.entry
	return entry != nil && entry.LastPing.Success && entry.LastPing.LatencyMS > 0 &&
		time.Duration(entry.LastPing.LatencyMS)*time.Millisecond <= SlowPingThreshold &&
		!entry.QuarantineUntil.After(ctx.now)
}

func matchMissingSpeedCategory(ctx matchContext) bool {
	return !hasSpeedMeasurement(ctx.entry)
}

func matchLongContextCategory(ctx matchContext) bool {
	return HasTag(ctx.entry, "long_context") || metadataContextLength(ctx.entry) >= 128_000
}

func matchToolsCategory(ctx matchContext) bool {
	return HasTag(ctx.entry, "tools") || metadataSupportsAny(ctx.entry, "tools", "tool_choice")
}

func matchStructuredOutputsCategory(ctx matchContext) bool {
	return HasTag(ctx.entry, "structured_outputs") || metadataSupportsAny(ctx.entry, "structured_outputs", "response_format")
}

func matchReasoningCategory(ctx matchContext) bool {
	return HasTag(ctx.entry, "reasoning") || metadataHasReasoning(ctx.entry) ||
		metadataSupportsAny(ctx.entry, "reasoning", "include_reasoning", "reasoning_effort")
}
