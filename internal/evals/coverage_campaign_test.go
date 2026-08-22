package evals

import (
	"strings"
	"testing"
)

func TestCoverageCampaignFrozenManifestAndProjectedRows(t *testing.T) {
	manifest, err := loadCoverageCampaignManifest()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(manifest.Targets), 11; got != want {
		t.Fatalf("targets=%d want %d", got, want)
	}
	rows := make([]ReportModel, 0, len(manifest.Targets))
	for _, target := range manifest.Targets {
		scores := make(map[string]*LlamboScore, len(categorySpecs))
		for _, category := range categorySpecs {
			scores[category.name] = nil
		}
		rows = append(rows, ReportModel{Key: target.Key, Projection: &ProjectionInfo{}, LlamboScores: scores})
	}
	rows[0].LlamboScores["reasoning"] = &LlamboScore{Score: 50}
	first, err := buildCoverageCampaign(rows)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildCoverageCampaign(rows)
	if err != nil {
		t.Fatal(err)
	}
	if first.TargetModels != 11 || first.TotalCells != 66 || first.PresentCells != 1 || len(first.Unresolved) != 65 || first.Status != "blocked" {
		t.Fatalf("unexpected campaign: %#v", first)
	}
	if first.Unresolved[0] != second.Unresolved[0] || first.Unresolved[len(first.Unresolved)-1] != second.Unresolved[len(second.Unresolved)-1] {
		t.Fatal("campaign ordering is not deterministic")
	}
	first.Unresolved[0].Reason = "needs <sealed>|official receipt"
	markdown, html := RenderMarkdown(Report{CoverageCampaign: first}, 0), RenderHTML(Report{CoverageCampaign: first}, 0)
	for _, want := range []string{"External score coverage", "Reason", "needs <sealed>\\|official receipt", "Inactive/new models", "Added after the frozen 11-target campaign"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("Markdown coverage campaign missing %q:\n%s", want, markdown)
		}
	}
	for _, want := range []string{"External score coverage", "Reason", "needs &lt;sealed&gt;|official receipt", "Inactive/new models", "Added after the frozen 11-target campaign"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML coverage campaign missing %q:\n%s", want, html)
		}
	}
}

func TestFilterProjectedRowsRefreshesCampaignCoverage(t *testing.T) {
	manifest, err := loadCoverageCampaignManifest()
	if err != nil {
		t.Fatal(err)
	}
	live, stale := manifest.Targets[0].Key, manifest.Targets[1].Key
	report := Report{Models: []ReportModel{
		{Key: "canonical", LlamboScores: map[string]*LlamboScore{}},
		{Key: live, Projection: &ProjectionInfo{}, LlamboScores: map[string]*LlamboScore{"reasoning": {Score: 50}}},
		{Key: stale, Projection: &ProjectionInfo{}, LlamboScores: map[string]*LlamboScore{"reasoning": {Score: 50}}},
	}}
	report.CoverageCampaign, err = buildCoverageCampaign(report.Models)
	if err != nil {
		t.Fatal(err)
	}
	FilterProjectedRowsToLiveOMLX(&report, OMLXDiscovery{Models: []string{live}}, nil)
	if report.CoverageCampaign == nil || report.CoverageCampaign.PresentCells != 1 || len(report.CoverageCampaign.Unresolved) != 65 {
		t.Fatalf("live filter did not refresh campaign coverage: %#v", report.CoverageCampaign)
	}
	if len(report.CoverageCampaign.InactiveOrNew) == 0 || report.CoverageCampaign.InactiveOrNew[0].Reason == "" {
		t.Fatalf("filter lost explicit inactive/new notes: %#v", report.CoverageCampaign)
	}

	report = Report{Models: []ReportModel{{Key: live, Projection: &ProjectionInfo{}, LlamboScores: map[string]*LlamboScore{"reasoning": {Score: 50}}}}}
	report.CoverageCampaign, err = buildCoverageCampaign(report.Models)
	if err != nil {
		t.Fatal(err)
	}
	FilterProjectedRowsToLiveOMLX(&report, OMLXDiscovery{}, errUnavailableOMLX{})
	if report.CoverageCampaign == nil || report.CoverageCampaign.PresentCells != 0 || len(report.CoverageCampaign.Unresolved) != 66 || report.CoverageCampaign.InactiveOrNew[0].Reason == "" {
		t.Fatalf("unavailable filter did not restore blocked campaign coverage: %#v", report.CoverageCampaign)
	}
}

type errUnavailableOMLX struct{}

func (errUnavailableOMLX) Error() string { return "offline" }

func TestBuildReportIncludesBlockedCoverageCampaign(t *testing.T) {
	report, err := BuildReport(Result{}, "matrix")
	if err != nil {
		t.Fatal(err)
	}
	if report.CoverageCampaign == nil || report.CoverageCampaign.TargetModels != 11 || report.CoverageCampaign.TotalCells != 66 || report.CoverageCampaign.Status != "blocked" {
		t.Fatalf("missing blocked campaign: %#v", report.CoverageCampaign)
	}
}
