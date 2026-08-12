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
