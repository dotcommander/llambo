package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

func TestWritingIterations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		benchmark string
		requested int
		want      int
		wantErr   bool
	}{
		{"writingbench", 0, 1, false},
		{"writingbench", 2, 0, true},
		{"eqbench-creative-v3", 0, 3, false},
		{"eqbench-creative-v3", 1, 1, false},
		{"eqbench-creative-v3", 4, 0, true},
		{"prose-screen", 0, 1, false},
		{"prose-screen", 2, 0, true},
	}
	for _, test := range tests {
		t.Run(test.benchmark, func(t *testing.T) {
			t.Parallel()
			got, err := writingIterations(test.benchmark, test.requested)
			if got != test.want || (err != nil) != test.wantErr {
				t.Fatalf("writingIterations(%q, %d) = %d, %v", test.benchmark, test.requested, got, err)
			}
		})
	}
}

func TestWritingJudgeTokenCap(t *testing.T) {
	prose := evals.ProseScreenAdapter{}
	if got, err := writingJudgeTokenCap(prose, 8192, false); err != nil || got != evals.DefaultProseEvaluationMaxTokens {
		t.Fatalf("default cap = %d, %v", got, err)
	}
	if got, err := writingJudgeTokenCap(prose, 8192, true); err != nil || got != 8192 {
		t.Fatalf("explicit override = %d, %v", got, err)
	}
	if got, err := writingJudgeTokenCap(prose, evals.DefaultProseEvaluationMaxTokens, true); err != nil || got != evals.DefaultProseEvaluationMaxTokens {
		t.Fatalf("explicit cap = %d, %v", got, err)
	}
	if got, err := writingJudgeTokenCap(evals.WritingBenchAdapter{}, 8192, true); err != nil || got != 8192 {
		t.Fatalf("legacy cap = %d, %v", got, err)
	}
}

func TestWritingExecutionContextPreservesExplicitControls(t *testing.T) {
	t.Parallel()
	cfg := providers.Config{
		Model: "reasoner",
		ExtraBody: map[string]any{
			"chat_template_kwargs": map[string]any{"configured": true},
		},
		ExtraBodyByModel: map[string]map[string]any{
			"reasoner": {"chat_template_kwargs": map[string]any{"per_model": true}},
		},
	}
	call := evals.WritingExecutionCall{
		Model:           evals.WritingModelSpec{Provider: "omlx", Model: "reasoner"},
		MaxOutputTokens: 8192,
		Settings: evals.WritingGenerationSettings{
			Temperature:    0,
			TemperatureSet: true,
			ExtraBody: map[string]any{
				"reasoning_effort":     "off",
				"thinking_budget":      0,
				"chat_template_kwargs": map[string]any{"enable_thinking": false},
			},
		},
	}
	ctx := writingExecutionContext(context.Background(), cfg, call)
	request := providers.ChatRequestOverridesFromContext(ctx)
	if request == nil || request.MaxTokens == nil || *request.MaxTokens != 8192 || request.Temperature == nil || *request.Temperature != 0 {
		t.Fatalf("request overrides = %#v", request)
	}
	overrides := providers.JSONOverridesFromContext(ctx)
	if overrides["reasoning_effort"] != "off" || overrides["thinking_budget"] != 0 {
		t.Fatalf("JSON overrides = %#v", overrides)
	}
	kwargs, ok := overrides["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["configured"] != true || kwargs["per_model"] != true || kwargs["enable_thinking"] != false {
		t.Fatalf("chat template overrides = %#v", overrides["chat_template_kwargs"])
	}
	if cfg.ExtraBody["reasoning_effort"] != nil {
		t.Fatalf("runtime config was mutated: %#v", cfg.ExtraBody)
	}
}

func TestApplyWritingReasoningSettingsIncludesThinkingBudget(t *testing.T) {
	t.Parallel()
	settings := evals.WritingGenerationSettings{ExtraBody: map[string]any{
		"chat_template_kwargs": map[string]any{"preserve_thinking": false},
	}}
	applyWritingReasoningSettings(&settings, "high", 32768)
	if settings.ExtraBody["reasoning_effort"] != "high" || settings.ExtraBody["thinking_budget"] != 32768 {
		t.Fatalf("reasoning controls = %#v", settings.ExtraBody)
	}
	kwargs, ok := settings.ExtraBody["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["enable_thinking"] != true {
		t.Fatalf("chat template controls = %#v", settings.ExtraBody["chat_template_kwargs"])
	}
	if kwargs["preserve_thinking"] != false {
		t.Fatalf("existing chat template controls were not preserved: %#v", kwargs)
	}
}

func TestWritingThinkingBudgetValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		budget   int
		effort   string
		explicit bool
		message  string
	}{
		{"negative", -1, "low", false, "--thinking-budget-tokens"},
		{"explicit zero", 0, "low", true, "--thinking-budget-tokens"},
		{"too large", 65537, "high", false, "--thinking-budget-tokens"},
		{"off conflict", 8192, "off", false, "cannot be combined"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := &evalsWritingRunCommand{ThinkingBudget: test.budget, ReasoningEffort: test.effort, JudgeTokens: 8192, Concurrency: 1, Timeout: time.Minute}
			err := runWritingEvaluationCommand(&commandIO{ctx: context.Background(), changed: map[string]bool{"thinking-budget-tokens": test.explicit}}, options, 1)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestWritingGenerationTokenOverrideValidation(t *testing.T) {
	t.Parallel()
	for _, value := range []int{-1, 0, 65537} {
		options := &evalsWritingRunCommand{GenerationTokens: value, JudgeTokens: 8192, Concurrency: 1, Timeout: time.Minute}
		err := runWritingEvaluationCommand(&commandIO{ctx: context.Background(), changed: map[string]bool{"generation-max-output-tokens": value == 0}}, options, 1)
		if err == nil || !strings.Contains(err.Error(), "--generation-max-output-tokens") {
			t.Fatalf("value %d error = %v", value, err)
		}
	}
}

func TestValidateWritingExecutionResult(t *testing.T) {
	t.Parallel()
	model := evals.WritingModelSpec{Provider: "openrouter", Model: "anthropic/claude-sonnet-5"}
	if err := validateWritingExecutionResult(model, model.Provider, model.Model, "ok"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		provider string
		model    string
		content  string
	}{
		{"provider drift", "other", model.Model, "ok"},
		{"model drift", model.Provider, "other", "ok"},
		{"empty response", model.Provider, model.Model, "  "},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateWritingExecutionResult(model, test.provider, test.model, test.content); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestWritingRunRejectsInheritedCatalogFlags(t *testing.T) {
	t.Parallel()
	io := &commandIO{ctx: context.Background(), changed: map[string]bool{"refresh": true}}
	err := (&evalsWritingRunCommand{}).Run(&evalsCommand{}, io)
	if err == nil || !strings.Contains(err.Error(), "--refresh is not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestExactWritingModelSelector(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{"openrouter/anthropic/claude", "deepseek/deepseek-chat"} {
		if !exactWritingModelSelector(selector) {
			t.Fatalf("expected exact selector %q", selector)
		}
	}
	for _, selector := range []string{"healthy", "tag:writing", "model-only", "/missing"} {
		if exactWritingModelSelector(selector) {
			t.Fatalf("unexpected exact selector %q", selector)
		}
	}
}

func TestGPTProLocalEvalPolicy(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{"openai/gpt-5.4-pro", "OPENAI/gpt-5-pro-2026-08-20"} {
		if !isGPTProModel(selector) {
			t.Fatalf("expected GPT Pro selector %q to be prohibited", selector)
		}
	}
	for _, selector := range []string{"openai/gpt-5.4", "openrouter/openai/gpt-5.4-pro", "omlx/gpt-5.4-pro"} {
		if isGPTProModel(selector) {
			t.Fatalf("unexpected GPT Pro policy match for %q", selector)
		}
	}
}

func TestWriteWritingDryRunHasZeroProviderCalls(t *testing.T) {
	t.Parallel()
	manifest := evals.WritingRunManifest{SchemaVersion: 1, RunID: "run", CreatedAt: time.Unix(1, 0)}
	plan := evals.WritingRunPlan{BenchmarkID: "writingbench", PromptCount: 1, GenerationCalls: 1, JudgmentCalls: 5}
	var out bytes.Buffer
	if err := writeWritingDryRun(&out, manifest, plan); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Mode          string `json:"mode"`
		ProviderCalls int    `json:"provider_calls"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Mode != "dry-run" || decoded.ProviderCalls != 0 {
		t.Fatalf("dry run = %#v", decoded)
	}
}

func TestWritingObservedCostFailsClosedWhenPaidUsageIsMissing(t *testing.T) {
	t.Parallel()
	paid := evals.WritingModelSpec{Provider: "synthetic", Model: "hf:openai/gpt-oss-120b", InputPer1M: 0.1, OutputPer1M: 0.1}
	manifest := evals.WritingRunManifest{Identity: evals.WritingRunIdentity{Models: []evals.WritingModelSpec{paid}}}
	record := evals.WritingGenerationRecord{Provider: paid.Provider, Model: paid.Model}
	if got := writingObservedCost(manifest, []evals.WritingGenerationRecord{record}, nil); got != nil {
		t.Fatalf("unknown paid usage settled as $%v", *got)
	}
	record.Usage = evals.WritingUsage{Known: true, CostUSD: 0.002}
	got := writingObservedCost(manifest, []evals.WritingGenerationRecord{record}, nil)
	if got == nil || *got != 0.002 {
		t.Fatalf("known paid usage = %v, want 0.002", got)
	}
}

func TestWritingRuntimeConfigUsesResolvedPricing(t *testing.T) {
	t.Parallel()
	model := evals.WritingModelSpec{InputPer1M: 0.75, OutputPer1M: 3.75}
	cfg := writingRuntimeConfig(providers.Config{InputCostPM: 99, OutputCostPM: 99}, model)
	if cfg.InputCostPM != model.InputPer1M || cfg.OutputCostPM != model.OutputPer1M {
		t.Fatalf("runtime pricing = (%v, %v), want (%v, %v)", cfg.InputCostPM, cfg.OutputCostPM, model.InputPer1M, model.OutputPer1M)
	}
}

func TestWritingExternalEvidenceDoesNotPromoteLocalPerformanceOrQuality(t *testing.T) {
	t.Parallel()
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"omlx": {Models: map[string]*catalog.ModelEntry{
			"local-only": {
				Quality:    map[string]catalog.QualityEvidence{"writing": {Score: 0.9, Source: ".work/local-report.json"}},
				Benchmarks: map[string]catalog.BenchmarkEvidence{"speed": {SpeedTokensPerSecond: 12, Source: "local-receipt.json"}},
			},
		}},
	}}
	class, _, _, err := writingExternalEvidence(cat, "omlx", "local-only")
	if err != nil {
		t.Fatal(err)
	}
	if class != "missing" {
		t.Fatalf("local-only evidence class = %q, want missing", class)
	}
}

func TestWritingExternalEvidenceAcceptsKnownExternalQualitySource(t *testing.T) {
	t.Parallel()
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"gemini": {Models: map[string]*catalog.ModelEntry{
			"model": {Quality: map[string]catalog.QualityEvidence{"overall": {Score: 0.8, Source: "artificial_analysis,llm_stats"}}},
		}},
	}}
	class, source, note, err := writingExternalEvidence(cat, "gemini", "model")
	if err != nil {
		t.Fatal(err)
	}
	if class != "exact" || source != "artificial_analysis,llm_stats" || note != "overall" {
		t.Fatalf("external evidence = %q, %q, %q", class, source, note)
	}
}
