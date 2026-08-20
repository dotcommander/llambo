package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalRunRejectsGPTProBeforeInputAccess(t *testing.T) {
	t.Parallel()
	err := runLocalEvaluationCommand(&commandIO{ctx: context.Background()}, &evalsLocalRunCommand{Model: "openai/gpt-5.4-pro"})
	if err == nil || !strings.Contains(err.Error(), "prohibited by user policy") {
		t.Fatalf("error = %v", err)
	}
}

func TestLocalRunDryRunHasNoProviderCalls(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := filepath.Join(dir, "sealed.json")
	cases := make([]string, 0, 10)
	for i := 1; i <= 10; i++ {
		expected := `{"name":"weather","arguments":{}}`
		if i > 8 {
			expected = "null"
		}
		cases = append(cases, fmt.Sprintf(`{"id":"case-%d","prompt":"Use weather.","tools":[{"name":"weather","parameters":{"type":"object"}}],"expected":%s}`, i, expected))
	}
	manifest := `{"suite":"tools","version":"v1","cases":[` + strings.Join(cases, ",") + `]}`
	if err := os.WriteFile(input, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	io := &commandIO{ctx: context.Background(), stdout: &out, changed: map[string]bool{"input-per-1m": true, "output-per-1m": true}}
	err := runLocalEvaluationCommand(io, &evalsLocalRunCommand{Suite: "tools", Input: input, Model: "groq/model", OutputDir: filepath.Join(dir, "run"), Limit: 1, Concurrency: 1, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"provider_calls":0`) {
		t.Fatalf("dry run output %s", out.String())
	}
}
