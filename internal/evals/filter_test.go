package evals

import (
	"strings"
	"testing"
)

func TestApplyMinimumCategoryScore(t *testing.T) {
	report := Report{Models: []ReportModel{
		{Key: "high", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80}}},
		{Key: "boundary", LlamboScores: map[string]*LlamboScore{"coding": {Score: 40}}},
		{Key: "low", LlamboScores: map[string]*LlamboScore{"coding": {Score: 39.9}}},
		{Key: "missing", LlamboScores: map[string]*LlamboScore{"coding": nil}},
		{Key: "local", Projection: &ProjectionInfo{SourceKey: "high"}, LlamboScores: map[string]*LlamboScore{"coding": {Score: 43.5}}},
		{Key: "estimated", Projection: &ProjectionInfo{SourceKey: "high"}, LlamboScores: map[string]*LlamboScore{"coding": {Score: 99, Estimated: true}}},
	}}

	ApplyCategoryEligibility(&report, "coding", 40, -1)

	if got := modelKeys(report.Models); strings.Join(got, ",") != "high,boundary,local" {
		t.Fatalf("unexpected eligible models: %v", got)
	}
	want := EligibilityDiagnostics{MinScore: 40, ScoreCategory: "coding", MaxOutputPrice: -1, InputModels: 6, IncludedModels: 3, ExcludedModels: 3, MissingPrimary: 2, BelowScore: 1}
	if report.Eligibility == nil || *report.Eligibility != want {
		t.Fatalf("unexpected eligibility diagnostics: %#v", report.Eligibility)
	}
	markdown := RenderMarkdown(report, 0)
	if !strings.Contains(markdown, "Eligibility filters: coding >= 40.0; 3 of 6 models included") {
		t.Fatalf("eligibility filter missing from Markdown:\n%s", markdown)
	}
}

func TestApplyEligibilityMaximumKnownOutputPrice(t *testing.T) {
	price := func(value float64) *float64 { return &value }
	report := Report{Models: []ReportModel{
		{Key: "cheap", Scores: map[string]*ExternalScore{"overall": {Score: 80}}, LLMStats: &LLMStatsMetrics{OutputPrice: price(5)}},
		{Key: "boundary", Scores: map[string]*ExternalScore{"overall": {Score: 80}}, AA: &ArtificialMetrics{OutputPrice: price(10)}},
		{Key: "expensive", Scores: map[string]*ExternalScore{"overall": {Score: 80}}, AA: &ArtificialMetrics{OutputPrice: price(11)}},
		{Key: "disagreement", Scores: map[string]*ExternalScore{"overall": {Score: 80}}, LLMStats: &LLMStatsMetrics{OutputPrice: price(5)}, AA: &ArtificialMetrics{OutputPrice: price(12)}},
		{Key: "unknown", Scores: map[string]*ExternalScore{"overall": {Score: 80}}},
		{Key: "local", Projection: &ProjectionInfo{SourceKey: "expensive"}, Scores: map[string]*ExternalScore{"overall": {Score: 80}}, AA: &ArtificialMetrics{OutputPrice: price(50)}},
	}}

	ApplyCategoryEligibility(&report, "coding", -1, 10)

	if got := strings.Join(modelKeys(report.Models), ","); got != "cheap,boundary,unknown,local" {
		t.Fatalf("unexpected price-eligible models: %s", got)
	}
	if report.Eligibility == nil || report.Eligibility.OverOutputPrice != 2 || report.Eligibility.UnknownPriceKept != 1 || report.Eligibility.IncludedModels != 4 {
		t.Fatalf("unexpected price diagnostics: %#v", report.Eligibility)
	}
}

func TestApplyMinimumScoreNegativeDisablesFilter(t *testing.T) {
	report := Report{Models: []ReportModel{{Key: "missing", Scores: map[string]*ExternalScore{}}}}
	ApplyCategoryEligibility(&report, "coding", -1, -1)
	if len(report.Models) != 1 || report.Eligibility != nil {
		t.Fatalf("negative threshold did not disable filter: %#v", report)
	}
}

func TestApplyEligibilityNegativeLimitsDisableAllFilters(t *testing.T) {
	report := Report{Models: []ReportModel{{Key: "missing", Scores: map[string]*ExternalScore{}}}}
	ApplyCategoryEligibility(&report, "matrix", -1, -1)
	if len(report.Models) != 1 || report.Eligibility != nil {
		t.Fatalf("negative limits did not disable filters: %#v", report)
	}
}

func modelKeys(models []ReportModel) []string {
	keys := make([]string, len(models))
	for i, model := range models {
		keys[i] = model.Key
	}
	return keys
}
