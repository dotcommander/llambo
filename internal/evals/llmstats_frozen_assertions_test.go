package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertFrozenCacheRejectsMissingAndCorruptArtifact(t *testing.T, opts Options, dir string) {
	t.Helper()
	result, err := fetchCachedOnly(opts, sourceSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	status := sourceStatusNamed(t, result.Sources, "LLM Stats frozen cohorts")
	if _, ok := result.Models[0].Benchmarks["longbench-v2"]; ok || status.Cache != "unavailable" || !strings.Contains(status.Error, "frozen LLM Stats cohorts unavailable") || len(result.Models) != 1 {
		t.Fatalf("missing sealed artifact did not fail closed while preserving cached models: %#v %#v", status, result.Models)
	}
	artifactPath := llmStatsFrozenCohortArtifactPath(dir)
	if err = os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(artifactPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = fetchCachedOnly(opts, sourceSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Models[0].Benchmarks["longbench-v2"]; ok || !strings.Contains(sourceStatusNamed(t, result.Sources, "LLM Stats frozen cohorts").Error, "digest") {
		t.Fatalf("corrupt sealed artifact did not fail closed: %#v", result)
	}
}
