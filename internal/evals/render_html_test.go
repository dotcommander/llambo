package evals

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHTMLIsAccessibleSortableEscapedAndSelfContained(t *testing.T) {
	score := &ExternalScore{Score: 88.5, Low: 80, High: 95, Coverage: .75, Confidence: "high"}
	unsafeName := `Model <script>alert("x")</script> & friends`
	unsafeError := `source failed <img src=x onerror=alert(1)>`
	projection := &ProjectionInfo{SourceKey: "upstream/model", Confidence: "medium", Basis: "reviewed", ReviewedAt: "2026-01-01T00:00:00Z"}
	report := Report{
		GeneratedAt:    time.Date(2026, time.August, 5, 3, 20, 0, 0, time.UTC),
		FormulaVersion: FormulaVersion,
		RankingProfile: "overall",
		AAVersion:      4.1,
		Sources: []SourceStatus{
			{Name: "safe", URL: "https://example.test/feed?a=1&b=2", Cache: "cached", Models: 2, FetchedAt: time.Unix(1, 0).UTC()},
			{Name: "unsafe", URL: "javascript:alert(1)", Cache: "unavailable", Error: unsafeError},
		},
		Eligibility: &EligibilityDiagnostics{MinOverall: 40, MaxOutputPrice: 2, InputModels: 3, IncludedModels: 2, ExcludedModels: 1, BelowOverall: 1, UnknownPriceKept: 1},
		Projections: ProjectionDiagnostics{RegistrySource: "embedded", Version: 1, Configured: 1, Applied: 1},
		OMLX:        &OMLXDiagnostics{Status: "unavailable", Error: "not configured"},
		Formula: FormulaDiagnostics{
			Method: "percentiles < neutral >", Population: 3, DualSourceModels: 2,
			Reference: FormulaReferenceSummary{CreatedAt: time.Unix(2, 0).UTC(), SourceModelCounts: map[string]int{"llm_stats": 10, "artificial_analysis": 11}},
			Drift:     DriftDiagnostics{Status: "stable", MaxKS: .012, Reasons: []string{"reason < one >"}},
			Stability: StabilityDiagnostics{Status: "stable", Variants: 4, MeanKendall: .9, MinTop20Overlap: .8},
			Profiles:  []ProfileDiagnostic{{Name: "overall", Weights: map[string]float64{"z": .2, "a": .8}}},
			Metrics:   []MetricDiagnostic{{Name: "metric <a>", Dimension: "coding", Source: "llm_stats", Weight: .5, Population: 2, ReferencePopulation: 10, Direction: "higher"}},
		},
		Models: []ReportModel{
			{Name: unsafeName, Organization: "Org & Co", Scores: map[string]*ExternalScore{"overall": score}},
			{Name: "Second canonical", Scores: map[string]*ExternalScore{"overall": {Score: 20}}},
			{Name: "Projected local", Projection: projection, Scores: map[string]*ExternalScore{"overall": score}},
		},
	}

	got := RenderHTML(report, 1)
	for _, want := range []string{
		"<!doctype html>", "<main", "<section", "<caption>Canonical model rankings</caption>",
		"data-sortable=\"true\"", "data-sort-key=\"overall\"", "data-type=\"number\"", "aria-sort=\"none\"", "<button type=\"button\"",
		"role=\"status\"", "aria-live=\"polite\"", "Formula and diagnostics", "Source status", "Tracked local/OSS projections", FormulaVersion,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("HTML missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, unsafeName) || strings.Contains(got, unsafeError) {
		t.Fatalf("HTML contains unescaped user content:\n%s", got)
	}
	for _, want := range []string{"Model &lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; &amp; friends", "source failed &lt;img src=x onerror=alert(1)&gt;", `href="https://example.test/feed?a=1&amp;b=2"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("HTML missing escaped content %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `href="javascript:`) {
		t.Fatal("unsafe source URL was emitted as a link")
	}
	if strings.Contains(got, "Second canonical") {
		t.Fatal("canonical limit did not exclude the second row")
	}
	if !strings.Contains(got, "Projected local") {
		t.Fatal("projected rows should remain visible outside the canonical limit")
	}
	if !strings.Contains(got, "addEventListener(\"click\"") || !strings.Contains(got, "Array.prototype.slice.call(table.tBodies[0].rows)") {
		t.Fatal("inline sortable vanilla JS is missing")
	}
}

func TestRenderHTMLIsDeterministic(t *testing.T) {
	report := Report{
		GeneratedAt: time.Unix(123, 0).UTC(), FormulaVersion: FormulaVersion, RankingProfile: "overall",
		Formula: FormulaDiagnostics{Profiles: []ProfileDiagnostic{{Name: "overall", Weights: map[string]float64{"z": .2, "a": .8}}}},
		Models:  []ReportModel{{Name: "one", Scores: map[string]*ExternalScore{"overall": {Score: 1}}}},
	}
	baseline := RenderHTML(report, 0)
	for i := 0; i < 20; i++ {
		if got := RenderHTML(report, 0); got != baseline {
			t.Fatalf("identical input produced different HTML on iteration %d", i)
		}
	}
}
