package cmd

import "github.com/dotcommander/llambo/internal/evals"

func applyWritingReasoningSettings(settings *evals.WritingGenerationSettings, effort string, thinkingBudget int) {
	if (effort == "" || effort == "default") && thinkingBudget == 0 {
		return
	}
	if settings.ExtraBody == nil {
		settings.ExtraBody = make(map[string]any)
	}
	if effort == "off" {
		settings.ExtraBody["reasoning_effort"] = "off"
		settings.ExtraBody["thinking_budget"] = 0
		setWritingThinkingEnabled(settings.ExtraBody, false)
		return
	}
	if effort != "" && effort != "default" {
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
