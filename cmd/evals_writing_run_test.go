package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
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
