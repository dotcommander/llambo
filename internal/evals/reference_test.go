package evals

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedFormulaReferenceIsComplete(t *testing.T) {
	reference, err := loadFormulaReference()
	if err != nil {
		t.Fatal(err)
	}
	if reference.FormulaVersion != FormulaVersion || len(reference.Metrics) != len(externalMetrics) {
		t.Fatalf("incomplete formula reference: version=%q metrics=%d", reference.FormulaVersion, len(reference.Metrics))
	}
	for _, metric := range externalMetrics {
		if len(reference.Metrics[metric.name]) == 0 {
			t.Fatalf("formula reference has no values for %s", metric.name)
		}
	}
}

func TestLoadFormulaReferenceRejectsUnsortedMetric(t *testing.T) {
	original := formulaReferenceJSON
	defer func() { formulaReferenceJSON = original }()
	var reference FormulaReference
	if err := json.Unmarshal(original, &reference); err != nil {
		t.Fatal(err)
	}
	name := externalMetrics[0].name
	reference.Metrics[name][0], reference.Metrics[name][len(reference.Metrics[name])-1] = reference.Metrics[name][len(reference.Metrics[name])-1], reference.Metrics[name][0]
	formulaReferenceJSON, _ = json.Marshal(reference)
	if _, err := loadFormulaReference(); err == nil || !strings.Contains(err.Error(), "not sorted") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestGenerateFormulaReferenceIsDeterministic(t *testing.T) {
	result := Result{
		GeneratedAt: time.Unix(99, 0).UTC(), AAVersion: 4.1,
		Sources: []SourceStatus{{Name: "LLM Stats", FetchedAt: time.Unix(10, 0).UTC()}, {Name: "Artificial Analysis", FetchedAt: time.Unix(20, 0).UTC()}},
		Models:  []Model{externalTestModel("a", 20, 20, 20, 20, 20), externalTestModel("b", 80, 80, 80, 80, 80)},
	}
	first, err := GenerateFormulaReference(result)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateFormulaReference(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same source data generated different formula references")
	}
}

func TestTwoSampleKS(t *testing.T) {
	closeTo(t, twoSampleKS([]float64{1, 2, 3}, []float64{1, 2, 3}), 0)
	if got := twoSampleKS([]float64{1, 2, 3}, []float64{10, 20, 30}); got < .99 {
		t.Fatalf("disjoint distributions have KS %v", got)
	}
}

func TestEvaluateReferenceDriftReportsVersionAndDistributionChanges(t *testing.T) {
	models := []Model{externalTestModel("a", 20, 20, 20, 20, 20), externalTestModel("b", 80, 80, 80, 80, 80)}
	reference := FormulaReference{
		AAVersion: 4.1, SourceModelCounts: map[string]int{"llm_stats": 2, "artificial_analysis": 2},
		Metrics: map[string][]float64{"metric": {1, 2, 3}},
	}
	stable := evaluateReferenceDrift(models, 4.1, reference, map[string][]float64{"metric": {1, 2, 3}})
	if stable.Status != "stable" {
		t.Fatalf("unchanged reference drifted: %#v", stable)
	}
	drifted := evaluateReferenceDrift(models, 5.0, reference, map[string][]float64{"metric": {10, 20, 30}})
	if drifted.Status != "drifted" || drifted.MaxKS < .99 || len(drifted.Reasons) < 2 {
		t.Fatalf("material changes were not reported: %#v", drifted)
	}
}

func TestReferenceCoverageDriftUsesSourceCoverageRate(t *testing.T) {
	models := make([]Model, 20)
	for i := range models {
		models[i] = Model{Key: string(rune('a' + i)), Name: "model", AA: &ArtificialMetrics{Intelligence: float64Ptr(float64(i))}}
	}
	reference := FormulaReference{
		SourceModelCounts: map[string]int{"artificial_analysis": 10},
		Metrics:           map[string][]float64{"aa_intelligence_general": {1, 2, 3, 4, 5}},
	}
	proportional := evaluateReferenceDrift(models, 0, reference, map[string][]float64{"aa_intelligence_general": {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}})
	for _, reason := range proportional.Reasons {
		if strings.Contains(reason, "coverage changed") {
			t.Fatalf("proportional source growth falsely changed coverage: %#v", proportional)
		}
	}
	reference.SourceModelCounts["artificial_analysis"] = 20
	reference.Metrics["aa_intelligence_general"] = []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	loss := evaluateReferenceDrift(models, 0, reference, map[string][]float64{"aa_intelligence_general": {1, 2, 3, 4, 5, 6, 7}})
	found := false
	for _, reason := range loss.Reasons {
		found = found || strings.Contains(reason, "coverage changed 15.0 percentage points")
	}
	if !found {
		t.Fatalf("coverage-rate loss was not reported: %#v", loss)
	}
}

func TestSourceFingerprintIncludesIdentityInputs(t *testing.T) {
	models := []Model{externalTestModel("key", 50, 50, 50, 50, 50)}
	before := sourceFingerprints(models)
	models[0].Name = "renamed"
	after := sourceFingerprints(models)
	if before["llm_stats"] == after["llm_stats"] || before["artificial_analysis"] == after["artificial_analysis"] {
		t.Fatalf("identity change did not change fingerprints: before=%v after=%v", before, after)
	}
}
