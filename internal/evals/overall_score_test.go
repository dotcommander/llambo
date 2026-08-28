package evals

import (
	"slices"
	"sort"
	"testing"
)

func TestApplyOMLXCategoryScoresNeverCreatesOverallComposite(t *testing.T) {
	report := Report{RankingProfile: "coding", Models: []ReportModel{{Key: "model", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: .5, Families: []string{"repository-editing", "live-synthesis"}}}, UnresolvedReasons: map[string]string{}}}}
	ApplyOMLXCategoryScores(&report, []OMLXScoreInput{{Key: "model"}, {Key: "missing"}})
	if report.Models[0].LlamboScore != nil || report.Models[1].LlamboScore != nil {
		t.Fatal("category assembly created a retired overall composite")
	}
	if report.CategoryRankings["coding"].Status != "official" || report.CategoryRankings["coding"].Winner != "model" {
		t.Fatalf("unexpected coding ranking: %#v", report.CategoryRankings["coding"])
	}
	if report.Models[1].LlamboScores == nil || report.Models[1].UnresolvedReasons["writing"] == "" {
		t.Fatalf("missing inventory row was not retained as unresolved: %#v", report.Models[1])
	}
}

func TestApplyOMLXCategoryScoresClearsStaleSubsetWinnerLabels(t *testing.T) {
	report := Report{RankingProfile: "coding", Models: []ReportModel{
		{Key: "kept", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: .5, Families: []string{"repository-editing", "live-synthesis"}, WinnerStatus: "official_co_winner"}}, UnresolvedReasons: map[string]string{}},
		{Key: "excluded", LlamboScores: map[string]*LlamboScore{"coding": {Score: 90, TrustedCoverage: .5, Families: []string{"repository-editing", "live-synthesis"}, WinnerStatus: "official"}}, UnresolvedReasons: map[string]string{}},
	}}
	ApplyOMLXCategoryScores(&report, []OMLXScoreInput{{Key: "kept"}})
	byKey := map[string]ReportModel{}
	for _, row := range report.Models {
		byKey[row.Key] = row
	}
	if got := byKey["excluded"].LlamboScores["coding"].WinnerStatus; got != "" {
		t.Fatalf("excluded row kept stale winner status %q", got)
	}
	if got := byKey["kept"].LlamboScores["coding"].WinnerStatus; got != "official" {
		t.Fatalf("kept row winner status = %q, want official", got)
	}
}

func TestCategoryRankingMixedFamilyTieKeepsSparseCoWinnerProvisional(t *testing.T) {
	rows := []ReportModel{
		{Key: "official", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: .5, Families: []string{"repository-editing", "live-synthesis"}}}},
		{Key: "sparse", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: .5, Families: []string{"repository-editing"}}}},
	}
	ranking := buildCategoryRankings(rows)["coding"]
	if ranking.Entries[0].WinnerStatus != "official" || ranking.Entries[1].WinnerStatus != "provisional_co_winner" {
		t.Fatalf("mixed-family tie winner status = %#v", ranking.Entries)
	}
}

func TestCategoryRankingSelectsOfficialTiedCandidateRegardlessOfKeyOrder(t *testing.T) {
	rows := []ReportModel{
		{Key: "a-sparse", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: .5, Families: []string{"repository-editing"}}}},
		{Key: "z-official", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: .5, Families: []string{"repository-editing", "live-synthesis"}}}},
	}
	ranking := buildCategoryRankings(rows)["coding"]
	if ranking.Status != "official" || ranking.Winner != "z-official" || ranking.Entries[0].WinnerStatus != "provisional_co_winner" || ranking.Entries[1].WinnerStatus != "official" {
		t.Fatalf("lexically first sparse candidate changed tied official outcome: %#v", ranking)
	}
}

func TestEstimatedProjectionIsExcludedFromCategoryRankings(t *testing.T) {
	rows := []ReportModel{
		{Key: "official", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80, TrustedCoverage: .5, Families: []string{"repository-editing", "live-synthesis"}}}},
		{Key: "estimated", Projection: &ProjectionInfo{Confidence: "low"}, LlamboScores: map[string]*LlamboScore{"coding": {Score: 90, Confidence: "low", Estimated: true, EstimateMethod: "cross-category-remote-shrink-v1"}}},
	}
	ranking := buildCategoryRankings(rows)["coding"]
	if ranking.Status != "official" || ranking.Winner != "official" || len(ranking.Entries) != 1 || ranking.Entries[0].Key != "official" || ranking.Entries[0].WinnerStatus != "official" {
		t.Fatalf("estimated projection affected category ranking: %#v", ranking)
	}
	if !rows[1].LlamboScores["coding"].Estimated || rows[1].LlamboScores["coding"].Score != 90 {
		t.Fatalf("estimate was removed instead of remaining advisory: %#v", rows[1].LlamboScores["coding"])
	}
}

func TestCategoryRankByTreatsEstimatedScoresAsUnranked(t *testing.T) {
	rows := []ReportModel{
		{Key: "estimated", Name: "Estimated", LlamboScores: map[string]*LlamboScore{"coding": {Score: 99, Estimated: true}}},
		{Key: "missing", Name: "Missing", LlamboScores: map[string]*LlamboScore{}},
		{Key: "direct", Name: "Direct", LlamboScores: map[string]*LlamboScore{"coding": {Score: 80}}},
	}
	sort.SliceStable(rows, func(i, j int) bool { return compareReportRows(rows[i], rows[j], "coding") })
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = row.Key
	}
	if !slices.Equal(keys, []string{"direct", "estimated", "missing"}) {
		t.Fatalf("estimated score affected rank-by ordering: %v", keys)
	}
}

func TestMatrixRankByTreatsEstimatedOnlyRowsAsUnscored(t *testing.T) {
	rows := []ReportModel{
		{Key: "estimated", Name: "Z Estimated", LlamboScores: map[string]*LlamboScore{"coding": {Score: 99, Estimated: true}}},
		{Key: "unresolved", Name: "A Unresolved", LlamboScores: map[string]*LlamboScore{}},
	}
	sort.SliceStable(rows, func(i, j int) bool { return compareReportRows(rows[i], rows[j], "matrix") })
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = row.Key
	}
	if !slices.Equal(keys, []string{"unresolved", "estimated"}) {
		t.Fatalf("estimated-only row affected matrix ordering: %v", keys)
	}
}
