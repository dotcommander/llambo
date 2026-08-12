package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWritingRunReportDeterministic(t *testing.T) {
	t.Parallel()
	manifest := testWritingManifest()
	manifest.Identity.BenchmarkID = "eqbench-creative-v3"
	generations := []WritingGenerationRecord{{Key: "g", PromptID: "one", Domain1: "Professional", Domain2: "Announcements", Iteration: 1, Provider: "p", Model: "m", Status: "success", Usage: WritingUsage{CostUSD: 0.01}}}
	judgments := []WritingJudgmentRecord{{Key: "j", GenerationKey: "g", Status: "success", Score: 8, Usage: WritingUsage{CostUSD: 0.02}}}
	adapter := EQCreativeLocalRubricAdapter{}
	dir := t.TempDir()
	store, err := OpenWritingRunStore(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendGeneration(generations[0]); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendJudgment(judgments[0]); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteWritingRunArtifacts(dir, manifest, adapter, generations, judgments, true, time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteWritingRunArtifacts(dir, manifest, adapter, generations, judgments, true, time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("report JSON changed across deterministic render")
	}
	report := BuildWritingRunReport(manifest, adapter, generations, judgments, true)
	if len(report.PromptScores) != 1 || len(report.DomainScores) != 2 || len(report.CriterionScores) != 1 {
		t.Fatalf("report breakdowns = prompts:%d domains:%d criteria:%d", len(report.PromptScores), len(report.DomainScores), len(report.CriterionScores))
	}
	markdown, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(markdown), "local-rubric", "not the official EQ-Bench Elo") {
		t.Fatalf("missing score warning:\n%s", markdown)
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
