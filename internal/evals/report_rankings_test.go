package evals

import "testing"

func TestReportCapabilityRankValueExcludesEstimates(t *testing.T) {
	t.Parallel()
	row := ReportModel{
		LlamboScores: map[string]*LlamboScore{"coding": {Score: 90, Estimated: true}},
		Scores:       map[string]*ExternalScore{"speed": {Score: 70}},
	}
	if reportRowRankValue(row, "coding") != nil {
		t.Fatal("estimated capability entered canonical ranking")
	}
	if score := reportRowRankValue(row, "speed"); score == nil || *score != 70 {
		t.Fatalf("operational ranking changed: %v", score)
	}
}

func TestCategoryWinnersRetainOfficialAndProvisionalTieStatuses(t *testing.T) {
	t.Parallel()
	rows := []ReportModel{
		{Key: "a", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: 1, Families: []string{"one"}}}},
		{Key: "b", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: 1, Families: []string{"one", "two"}}}},
		{Key: "c", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: 1, Families: []string{"one", "two"}}}},
	}
	ranking := buildCategoryRankings(rows)["coding"]
	if ranking.Winner != "b" || ranking.Status != "official" {
		t.Fatalf("official family evidence did not decide leader tie: %#v", ranking)
	}
	want := []string{"provisional_co_winner", "official", "official_co_winner"}
	for i, entry := range ranking.Entries {
		if entry.WinnerStatus != want[i] {
			t.Fatalf("tie status %s: got %s want %s", entry.Key, entry.WinnerStatus, want[i])
		}
	}
}
