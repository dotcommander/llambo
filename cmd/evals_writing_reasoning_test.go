package cmd

import (
	"reflect"
	"testing"
)

func TestGeminiReasoningOverrides(t *testing.T) {
	t.Parallel()

	effort := map[string]any{"reasoning_effort": "low"}
	got := geminiReasoningOverrides(effort, "gemini-3.6-flash")
	thinking := got["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
	if thinking["thinkingLevel"] != "low" || len(thinking) != 1 {
		t.Fatalf("effort low thinkingConfig = %#v", thinking)
	}
	if _, ok := got["reasoning_effort"]; ok {
		t.Fatalf("reasoning_effort leaked into gemini payload: %#v", got)
	}

	offThree := geminiReasoningOverrides(map[string]any{"reasoning_effort": "off"}, "gemini-3.6-flash")
	offThreeConfig := offThree["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
	if offThreeConfig["thinkingLevel"] != "minimal" || offThreeConfig["includeThoughts"] != false {
		t.Fatalf("off on 3.6 flash = %#v", offThreeConfig)
	}

	offTwoFive := geminiReasoningOverrides(map[string]any{"reasoning_effort": "off"}, "gemini-2.5-flash")
	offTwoFiveConfig := offTwoFive["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
	if offTwoFiveConfig["thinkingBudget"] != 0 || offTwoFiveConfig["includeThoughts"] != false {
		t.Fatalf("off on 2.5 flash = %#v", offTwoFiveConfig)
	}

	// Stored manifests round-trip ExtraBody numbers as float64.
	budget := geminiReasoningOverrides(map[string]any{"thinking_budget": float64(4096), "chat_template_kwargs": map[string]any{"enable_thinking": true}}, "gemini-2.5-pro")
	budgetConfig := budget["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
	if budgetConfig["thinkingBudget"] != 4096 {
		t.Fatalf("budget thinkingConfig = %#v", budgetConfig)
	}
	if _, ok := budget["chat_template_kwargs"]; ok {
		t.Fatalf("chat_template_kwargs leaked into gemini payload: %#v", budget)
	}

	untouched := map[string]any{"other": true}
	if same := geminiReasoningOverrides(untouched, "gemini-3.6-flash"); !reflect.DeepEqual(same, untouched) {
		t.Fatalf("no-reasoning map changed: %#v", same)
	}
	if got := geminiReasoningOverrides(nil, "gemini-3.6-flash"); got != nil {
		t.Fatalf("nil overrides = %#v, want nil", got)
	}
}
