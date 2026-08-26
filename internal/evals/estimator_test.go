package evals

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
)

func TestLLAMBO9EstimatorArtifactFingerprintAndCandidates(t *testing.T) {
	artifact, err := loadCategoryEstimator()
	if err != nil {
		t.Fatal(err)
	}
	const want = "9c9c8a28429d6c0d85214386565e24a1ee0d5cabb1ead3244f946f100d64db3f"
	if artifact.Fingerprint != want || estimatorFingerprint(artifact) != want {
		t.Fatalf("estimator fingerprint drifted: %#v", artifact)
	}
	wantSelection := []struct {
		method string
		alpha  float64
	}{{"ridge", 1}, {"ridge", .1}, {"ridge", .01}, {"organization-balanced-prior", 0}, {"ridge", .01}, {"organization-balanced-prior", 0}}
	for i, target := range artifact.Targets {
		if target.Method != wantSelection[i].method || target.Alpha != wantSelection[i].alpha {
			t.Fatalf("target %s selection drifted: %#v", target.Category, target)
		}
	}
	for _, row := range artifact.Rows {
		if row.Key == "" || row.Organization == "" {
			t.Fatalf("unidentified training row: %#v", row)
		}
		for category, score := range row.Scores {
			if !isCapabilityCategory(category) || !finite(score) || score < 0 || score > 100 {
				t.Fatalf("inadmissible training cell %s/%s=%v", row.Key, category, score)
			}
		}
	}
}

func TestLLAMBO9EstimatesOnlyMissingOMLXCellsWithoutRecursion(t *testing.T) {
	artifact, err := loadCategoryEstimator()
	if err != nil {
		t.Fatal(err)
	}
	direct := &LlamboScore{Score: 81.25, Coverage: .25, TrustedCoverage: .125, Confidence: "low", Families: []string{"repository-editing"}}
	rows := []ReportModel{{Key: "local", Projection: &ProjectionInfo{SourceKey: "reviewed-upstream"}, LlamboScores: map[string]*LlamboScore{"coding": direct}, UnresolvedReasons: map[string]string{"writing": "missing"}}}
	estimateOMLXRows(rows, artifact)
	if rows[0].LlamboScores["coding"] != direct || rows[0].LlamboScores["coding"].Score != 81.25 {
		t.Fatalf("direct score changed: %#v", rows[0].LlamboScores["coding"])
	}
	for _, category := range categorySpecs {
		score := rows[0].LlamboScores[category.name]
		if score == nil || !finite(score.Score) || score.Score < 0 || score.Score > 100 {
			t.Fatalf("missing or out-of-bounds score %s: %#v", category.name, score)
		}
		if category.name == "coding" {
			continue
		}
		if !score.Estimated || score.Coverage != 0 || score.TrustedCoverage != 0 || score.Confidence != "low" || len(score.Contributions) != 0 || len(score.Families) != 0 || score.EstimateCalibrationFingerprint != artifact.Fingerprint {
			t.Fatalf("estimate invented evidence for %s: %#v", category.name, score)
		}
		if slices.Contains(score.EstimateSources, category.name) || !slices.Equal(score.EstimateSources, []string{"coding"}) {
			t.Fatalf("recursive estimate sources for %s: %#v", category.name, score.EstimateSources)
		}
	}
}

func TestLLAMBO9PriorOnlyFallbackAndByteIdenticalRegeneration(t *testing.T) {
	artifact, err := loadCategoryEstimator()
	if err != nil {
		t.Fatal(err)
	}
	makeRows := func() []ReportModel {
		return []ReportModel{{Key: "unknown", Projection: &ProjectionInfo{}, LlamboScores: map[string]*LlamboScore{}, UnresolvedReasons: map[string]string{}}}
	}
	left, right := makeRows(), makeRows()
	estimateOMLXRows(left, artifact)
	estimateOMLXRows(right, artifact)
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	if string(a) != string(b) {
		t.Fatalf("regeneration differs:\n%s\n%s", a, b)
	}
	for _, score := range left[0].LlamboScores {
		if score.EstimateMethod != "prior-only" || len(score.EstimateSources) != 0 || math.IsNaN(score.Score) {
			t.Fatalf("invalid prior-only estimate: %#v", score)
		}
	}
}

func TestLLAMBO9SevenModelSnapshotIs22DirectPlus20Estimated(t *testing.T) {
	artifact, err := loadCategoryEstimator()
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]ReportModel, 7)
	directCount := 0
	for i := range rows {
		rows[i] = ReportModel{Key: string(rune('a' + i)), Projection: &ProjectionInfo{SourceKey: "reviewed"}, LlamboScores: map[string]*LlamboScore{}, UnresolvedReasons: map[string]string{}}
		limit := 3
		if i == 0 {
			limit = 4
		}
		for j := 0; j < limit; j++ {
			category := categorySpecs[j].name
			rows[i].LlamboScores[category] = &LlamboScore{Score: float64(10*i + j), Coverage: .25, TrustedCoverage: .125, Confidence: "low", Families: []string{"direct"}}
			directCount++
		}
	}
	before, _ := json.Marshal(rows)
	estimateOMLXRows(rows, artifact)
	benchmarkBacked, estimated, unresolved := 0, 0, 0
	for _, row := range rows {
		for _, category := range categorySpecs {
			score := row.LlamboScores[category.name]
			switch {
			case score == nil:
				unresolved++
			case score.Estimated:
				estimated++
			default:
				benchmarkBacked++
			}
		}
	}
	if directCount != 22 || benchmarkBacked != 22 || estimated != 20 || unresolved != 0 {
		t.Fatalf("matrix counts direct=%d backed=%d estimated=%d unresolved=%d", directCount, benchmarkBacked, estimated, unresolved)
	}
	var original []ReportModel
	if err := json.Unmarshal(before, &original); err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		for category, score := range original[i].LlamboScores {
			if score != nil && rows[i].LlamboScores[category].Score != score.Score {
				t.Fatalf("direct score changed at %s/%s", rows[i].Key, category)
			}
		}
	}
}
