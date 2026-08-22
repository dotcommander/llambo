package evals

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHTMLExposesMatrixEvidenceAndEscapes(t *testing.T) {
	raw, percentile, agreement := 8.7, 84.2, 91.0
	unsafeName := `Model <script>alert("x")</script> & friends`
	report := Report{
		GeneratedAt: time.Unix(1, 0).UTC(), FormulaVersion: FormulaVersion, RankingProfile: "matrix",
		Models: []ReportModel{{Name: unsafeName, LlamboScores: map[string]*LlamboScore{"writing": {
			Score: 84.2, Primary: &benchmarkEvidence{Benchmark: "writingbench", RawScore: &raw, Percentile: &percentile, ReferencePopulation: 17, SourceVersion: "v1", ContentSHA: "abc"},
			Checks: []benchmarkEvidence{{Benchmark: "eqbench-creative-v3", RawScore: &raw, Percentile: &percentile}}, Agreement: &agreement, Confidence: "high",
		}}}},
	}
	got := RenderHTML(report, 0)
	for _, want := range []string{"<!doctype html>", "data-sortable=\"true\"", "Llambo Score Matrix", "writingbench", "84.2", "frozen population 17", "high", FormulaVersion} {
		if !strings.Contains(got, want) {
			t.Fatalf("HTML missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, unsafeName) || !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("HTML did not escape model identity:\n%s", got)
	}
}

func TestRenderEvidenceIncludesFrozenPopulationAcrossFormats(t *testing.T) {
	raw, percentile := 0.61, 70.0
	report := Report{Models: []ReportModel{{Name: "target", LlamboScores: map[string]*LlamboScore{"long-context": {
		Score: 70, Contributions: []benchmarkEvidence{{Benchmark: "longbench-v2", Family: "multi-document-reasoning", RawScore: &raw, Percentile: &percentile, ReferencePopulation: 17}},
	}}}}}
	for name, rendered := range map[string]string{"markdown": RenderMarkdown(report, 0), "html": RenderHTML(report, 0)} {
		if !strings.Contains(rendered, "frozen population 17") {
			t.Fatalf("%s omitted the frozen reference population:\n%s", name, rendered)
		}
	}
}

func TestRenderHTMLIsDeterministic(t *testing.T) {
	report := Report{GeneratedAt: time.Unix(123, 0).UTC(), FormulaVersion: FormulaVersion, RankingProfile: "matrix", Models: []ReportModel{{Name: "one"}}}
	baseline := RenderHTML(report, 0)
	for i := 0; i < 20; i++ {
		if got := RenderHTML(report, 0); got != baseline {
			t.Fatalf("identical input produced different HTML on iteration %d", i)
		}
	}
}
