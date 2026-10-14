package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func assertEvalsMarkdownEncoding(t *testing.T) {
	t.Helper()
	t.Parallel()
	report := evals.Report{GeneratedAt: time.Unix(1, 0).UTC(), FormulaVersion: "test", RankingProfile: "matrix", Models: []evals.ReportModel{
		{Name: "A", Scores: map[string]*evals.ExternalScore{"speed": {Score: 75}}},
		{Name: "B"},
		{Name: "Local A", Projection: &evals.ProjectionInfo{SourceKey: "a", Confidence: "medium"}},
	}}
	markdown, err := encodeEvalsReport(report, "markdown", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "| A |") || strings.Contains(string(markdown), "| B |") {
		t.Fatalf("unexpected markdown:\n%s", markdown)
	}
}

func assertEvalsJSONEncoding(t *testing.T) {
	t.Helper()
	t.Parallel()
	report := evals.Report{GeneratedAt: time.Unix(1, 0).UTC(), FormulaVersion: "test", RankingProfile: "matrix", Models: []evals.ReportModel{
		{Name: "A", Scores: map[string]*evals.ExternalScore{"speed": {Score: 75}}},
		{Name: "B"},
		{Name: "Local A", Projection: &evals.ProjectionInfo{SourceKey: "a", Confidence: "medium"}},
	}}
	jsonData, err := encodeEvalsReport(report, "json", 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(jsonData), `"name": "B"`) {
		t.Fatalf("limit not applied: %s", jsonData)
	}
	if !strings.Contains(string(jsonData), `"operational_scores"`) || !strings.Contains(string(jsonData), `"speed"`) {
		t.Fatalf("JSON omitted separate operational scores: %s", jsonData)
	}
	if !strings.Contains(string(jsonData), `"local_projections"`) || !strings.Contains(string(jsonData), `"name": "Local A"`) {
		t.Fatalf("tracked projection was not kept in its separate JSON section: %s", jsonData)
	}
}

func assertEvalsHTMLEncoding(t *testing.T) {
	t.Helper()
	t.Parallel()
	report := evals.Report{GeneratedAt: time.Unix(1, 0).UTC(), FormulaVersion: "test", RankingProfile: "matrix", Models: []evals.ReportModel{
		{Name: "A", Scores: map[string]*evals.ExternalScore{"speed": {Score: 75}}},
		{Name: "B"},
		{Name: "Local A", Projection: &evals.ProjectionInfo{SourceKey: "a", Confidence: "medium"}},
	}}
	htmlData, err := encodeEvalsReport(report, "html", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(htmlData) == 0 || !strings.Contains(string(htmlData), "<") {
		t.Fatalf("unexpected HTML report: %s", htmlData)
	}
	if strings.Contains(string(htmlData), `data-value="b">B</td>`) || !strings.Contains(string(htmlData), "Local A") || !strings.Contains(string(htmlData), "data-sortable=\"true\"") {
		t.Fatalf("HTML limit or sortable markup was not preserved: %s", htmlData)
	}
}

func assertEvalsUnsupportedEncoding(t *testing.T) {
	t.Helper()
	t.Parallel()
	report := evals.Report{GeneratedAt: time.Unix(1, 0).UTC(), FormulaVersion: "test", RankingProfile: "matrix", Models: []evals.ReportModel{
		{Name: "A", Scores: map[string]*evals.ExternalScore{"speed": {Score: 75}}},
		{Name: "B"},
		{Name: "Local A", Projection: &evals.ProjectionInfo{SourceKey: "a", Confidence: "medium"}},
	}}
	if _, err := encodeEvalsReport(report, "yaml", 1); err == nil || !strings.Contains(err.Error(), "supported: markdown, json, html") {
		t.Fatalf("unexpected unsupported format error: %v", err)
	}
}
