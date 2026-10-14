package cmd

import (
	"maps"
	"regexp"
	"strconv"
	"strings"

	"github.com/dotcommander/llambo/internal/evals"
)

// Repeated string literals consolidated by goconst.
const (
	defaultStr = "default"
	off1       = "off"
)

func applyWritingReasoningSettings(settings *evals.WritingGenerationSettings, effort string, thinkingBudget int) {
	if (effort == "" || effort == defaultStr) && thinkingBudget == 0 {
		return
	}
	if settings.ExtraBody == nil {
		settings.ExtraBody = make(map[string]any)
	}
	if effort == off1 {
		settings.ExtraBody["reasoning_effort"] = off1
		settings.ExtraBody["thinking_budget"] = 0
		setWritingThinkingEnabled(settings.ExtraBody, false)
		return
	}
	if effort != "" && effort != defaultStr {
		settings.ExtraBody["reasoning_effort"] = effort
	}
	if thinkingBudget > 0 {
		settings.ExtraBody["thinking_budget"] = thinkingBudget
		setWritingThinkingEnabled(settings.ExtraBody, true)
	}
}

func setWritingThinkingEnabled(extraBody map[string]any, enabled bool) {
	kwargs := make(map[string]any)
	if existing, ok := extraBody["chat_template_kwargs"].(map[string]any); ok {
		for key, value := range existing {
			kwargs[key] = value
		}
	}
	kwargs["enable_thinking"] = enabled
	extraBody["chat_template_kwargs"] = kwargs
}

// geminiMinimalLevelModels lists Gemini 3 models whose documented thinkingLevel
// floor is minimal; the remaining documented 3-series models bottom out at low.
// Source: https://aistudio.google.com/docs/thinking#thinking-levels
var geminiMinimalLevelModels = map[string]bool{
	"gemini-3.6-flash":             true,
	"gemini-3.5-flash":             true,
	"gemini-3.5-flash-lite":        true,
	"gemini-3-flash-preview":       true,
	"gemini-robotics-er-2-preview": true,
}

var geminiMajorVersionPattern = regexp.MustCompile(`^gemini-(\d+)`)

func geminiBaseModel(model string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "models/")
}

// geminiUsesThinkingLevel reports whether the model controls thinking through
// thinkingLevel (Gemini 3 series) rather than the legacy thinkingBudget knob.
func geminiUsesThinkingLevel(model string) bool {
	match := geminiMajorVersionPattern.FindStringSubmatch(geminiBaseModel(model))
	if match == nil {
		return false
	}
	major, err := strconv.Atoi(match[1])
	return err == nil && major >= 3
}

// geminiThinkingDisabled renders the disable-thinking override for one model.
// Gemini 3 models cannot turn thinking off, so they get the lowest supported
// level with thought summaries suppressed; 2.5-series models keep the
// thinkingBudget:0 hard off switch.
func geminiThinkingDisabled(model string) map[string]any {
	if geminiUsesThinkingLevel(model) {
		level := "low"
		if geminiMinimalLevelModels[geminiBaseModel(model)] {
			level = "minimal"
		}
		return map[string]any{"thinkingLevel": level, "includeThoughts": false}
	}
	return map[string]any{"thinkingBudget": 0, "includeThoughts": false}
}

// geminiReasoningOverrides translates the OpenAI-shaped reasoning override
// keys into Gemini's generationConfig.thinkingConfig for native Gemini
// candidates. The generateContent endpoint rejects unknown top-level fields,
// so reasoning_effort/thinking_budget/chat_template_kwargs must not reach the
// Gemini payload as-is. Maps with no reasoning keys are returned unchanged.
func geminiReasoningOverrides(overrides map[string]any, model string) map[string]any {
	if overrides == nil {
		return nil
	}
	thinking := map[string]any{}
	if budget, ok := numericOverride(overrides["thinking_budget"]); ok && budget > 0 {
		thinking["thinkingBudget"] = budget
	}
	switch effort, _ := overrides["reasoning_effort"].(string); effort {
	case "", defaultStr:
	case off1:
		for key, value := range geminiThinkingDisabled(model) {
			thinking[key] = value
		}
	default:
		thinking["thinkingLevel"] = effort
	}
	if len(thinking) == 0 {
		return overrides
	}
	out := maps.Clone(overrides)
	delete(out, "reasoning_effort")
	delete(out, "thinking_budget")
	delete(out, "chat_template_kwargs")
	generation := map[string]any{}
	if existing, ok := out["generationConfig"].(map[string]any); ok {
		generation = maps.Clone(existing)
	}
	generation["thinkingConfig"] = thinking
	out["generationConfig"] = generation
	return out
}

// numericOverride reads an override integer that may have round-tripped
// through stored manifest JSON (float64) or been set in Go (int).
func numericOverride(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}
