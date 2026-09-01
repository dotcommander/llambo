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
	if _, err := WriteWritingRunArtifacts(dir, manifest, adapter, generations, judgments, true, time.Unix(20, 0)); err == nil {
		t.Fatal("immutable completed output accepted a rewrite")
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

func TestBuildWritingRunReportMarksProseScreenAndComputesAggregate(t *testing.T) {
	adapter := ProseScreenAdapter{}
	results, err := adapter.ParseJudgment(string(mustProseJSON(t, validProseEvaluation())), proseInput().Prose, nil)
	if err != nil {
		t.Fatal(err)
	}
	generation := WritingGenerationRecord{Key: "g", Status: "success", Provider: "provider", Model: "writer", PromptID: "case", Iteration: 1}
	judgment := WritingJudgmentRecord{Status: "success", GenerationKey: "g", Results: results}
	report := BuildWritingRunReport(WritingRunManifest{RunID: "run", Identity: WritingRunIdentity{BenchmarkID: adapter.ID()}}, adapter, []WritingGenerationRecord{generation}, []WritingJudgmentRecord{judgment}, true)
	if !report.PreScreenOnly || len(report.CriterionScores) != 6 || len(report.ModelAggregates) != 1 || report.ModelAggregates[0].Score != 4.1 {
		t.Fatalf("report = %#v", report)
	}
	if len(report.ModelDispersion) != 1 || !report.ModelDispersion[0].InsufficientSample {
		t.Fatalf("dispersion = %#v", report.ModelDispersion)
	}
	if markdown := RenderWritingRunMarkdown(report); !strings.Contains(markdown, "Pre-screen only") || !strings.Contains(markdown, "Go-computed prose aggregates") || !strings.Contains(markdown, "insufficient sample") {
		t.Fatalf("markdown = %s", markdown)
	}
}

func TestProseScreenArtifactsSuppressQualityImportAndUsePreScreenStatus(t *testing.T) {
	adapter := ProseScreenAdapter{}
	results, err := adapter.ParseJudgment(string(mustProseJSON(t, validProseEvaluation())), proseInput().Prose, nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest := WritingRunManifest{SchemaVersion: WritingRunSchemaVersion, RunID: "prose", Identity: WritingRunIdentity{BenchmarkID: adapter.ID(), InputSHA256: "hash", AdapterVersion: adapter.Version(), JudgePromptVersion: adapter.JudgePromptVersion(), GenerationPromptVersion: WritingGenerationPromptVersion}}
	dir := t.TempDir()
	store, err := OpenWritingRunStore(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	generation := WritingGenerationRecord{Key: "g", Status: "success", Provider: "p", Model: "writer", PromptID: "case"}
	judgment := WritingJudgmentRecord{Status: "success", GenerationKey: "g", Results: results}
	receipt, err := WriteWritingRunArtifacts(dir, manifest, adapter, []WritingGenerationRecord{generation}, []WritingJudgmentRecord{judgment}, true, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "complete_pre_screen" {
		t.Fatalf("status = %q", receipt.Status)
	}
	quality, err := os.ReadFile(filepath.Join(dir, "quality-import.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(quality) != "[]\n" {
		t.Fatalf("quality import = %s", quality)
	}
}
