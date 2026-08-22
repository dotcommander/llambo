package evals

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedFormulaReferenceIsComplete(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256(formulaReferenceJSON)); got != formulaReferenceDigest {
		t.Fatalf("LLAMBO-2 frozen reference changed without a formula-version bump: %s", got)
	}
	reference, err := loadFormulaReference()
	if err != nil {
		t.Fatal(err)
	}
	if reference.FormulaVersion != "LLAMBO-2" {
		t.Fatalf("incomplete formula reference: version=%q metrics=%d", reference.FormulaVersion, len(reference.Metrics))
	}
	if !reference.CreatedAt.Equal(time.Date(2026, 8, 21, 6, 11, 58, 0, time.UTC)) {
		t.Fatalf("unexpected LLAMBO-2 reference timestamp: %s", reference.CreatedAt)
	}
	for _, name := range frozenReferenceMetrics {
		if len(reference.Metrics[name]) == 0 {
			t.Fatalf("formula reference has no values for %s", name)
		}
	}
	if reference.SourceModelCounts["writingbench"] != 54 || reference.SourceModelCounts["eqbench_creative_v3"] != 125 || reference.SourceModelCounts["ifeval_official"] != 8 || reference.SourceFingerprints["writingbench"] != "623a59886c6b755724828a5e302df30e93b5aa30cd86bfff09ec512746d4b3d9" || reference.SourceFingerprints["eqbench_creative_v3"] != "92abdaab481827faa2629a9b8c14a7732b93db5a3a12d1a751ba6a81a1c48e96" || reference.SourceFingerprints["ifeval_official"] != "10950a8975d3f43c4b51a74def690603ac82be6c53a2d8eb509e6e31177b6efc" {
		t.Fatalf("WritingBench/EQ frozen provenance changed: %#v %#v", reference.SourceModelCounts, reference.SourceFingerprints)
	}
}

func TestEmbeddedLLMStatsFrozenCohortsAreComplete(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256(llmStatsFrozenCohortsJSON)); got != llmStatsFrozenCohortsDigest {
		t.Fatalf("frozen LLM Stats cohorts changed without a formula-version bump: %s", got)
	}
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"longbench-v2": 17, "math": 71, "humaneval": 66, "ifeval": 67, "arena-hard": 26, "osworld": 20}
	if cohorts.SealedArtifactSHA256 != llmStatsFrozenCohortsArtifactDigest || len(cohorts.Benchmarks) != len(want) {
		t.Fatalf("unexpected sealed cohort manifest: %#v", cohorts)
	}
	for benchmark, population := range want {
		cohort, ok := cohorts.Benchmarks[benchmark]
		if !ok || cohort.RowCount != population || len(cohort.Scores) != population {
			t.Fatalf("%s population = %#v, want %d", benchmark, cohort, population)
		}
	}
	if got := len(frozenBenchmarkNames()); got != 28 {
		t.Fatalf("frozen benchmark names = %d, want 28", got)
	}
}

func TestFrozenLLMStatsCohortRejectsUndersizedPopulation(t *testing.T) {
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		t.Fatal(err)
	}
	cohort := cohorts.Benchmarks["longbench-v2"]
	cohort.Scores = cohort.Scores[:4]
	cohorts.Benchmarks["longbench-v2"] = cohort
	if err := validateFrozenLLMStatsCohorts(cohorts); err == nil || !strings.Contains(err.Error(), "invalid contract") {
		t.Fatalf("undersized frozen cohort was accepted: %v", err)
	}
}

func TestLoadFormulaReferenceRejectsUnsortedMetric(t *testing.T) {
	var reference FormulaReference
	if err := json.Unmarshal(formulaReferenceJSON, &reference); err != nil {
		t.Fatal(err)
	}
	name := externalMetrics[0].name
	reference.Metrics[name][0], reference.Metrics[name][len(reference.Metrics[name])-1] = reference.Metrics[name][len(reference.Metrics[name])-1], reference.Metrics[name][0]
	if err := validateFormulaReference(reference); err == nil || !strings.Contains(err.Error(), "not sorted") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestLoadFormulaReferenceRejectsChangedDigest(t *testing.T) {
	original := formulaReferenceJSON
	defer func() { formulaReferenceJSON = original }()
	formulaReferenceJSON = append([]byte(nil), original...)
	formulaReferenceJSON[len(formulaReferenceJSON)-1] ^= 1
	if _, err := loadFormulaReference(); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestFormulaReferenceRejectsExtraMetric(t *testing.T) {
	var reference FormulaReference
	if err := json.Unmarshal(formulaReferenceJSON, &reference); err != nil {
		t.Fatal(err)
	}
	reference.Metrics["unreviewed"] = []float64{1}
	if err := validateFormulaReference(reference); err == nil || !strings.Contains(err.Error(), "exact inventory") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestUnsealedOrderedMetricsRemainInactiveInLLAMBO2(t *testing.T) {
	for _, metric := range []string{"bfcl_v4", "tau2_bench", "tau3_bench", "toolsandbox", "livecodebench_v6", "ifbench", "multi_if", "ifstruct", "longbench_v2", "ruler", "helmet", "aime_versioned"} {
		if containsFrozenReferenceMetric(metric) {
			t.Fatalf("unsealed ordered metric %q became active in LLAMBO-2", metric)
		}
	}
	data, err := GenerateFormulaReference(Result{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"formula_version": "LLAMBO-2"`) {
		t.Fatalf("reference generation changed the sealed formula version: %s", data)
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

func TestGenerateFormulaReferenceUsesSourceNativePopulation(t *testing.T) {
	value := 75.0
	result := Result{
		Models:          []Model{{Key: "joined"}},
		ReferenceModels: []Model{{Key: "wb", Benchmarks: map[string]BenchmarkResult{"writingbench": {Score: &value, ContentSHA: strings.Repeat("a", 64)}}}},
	}
	data, err := GenerateFormulaReference(result)
	if err != nil {
		t.Fatal(err)
	}
	var reference FormulaReference
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if reference.SourceModelCounts["writingbench"] != 1 || len(reference.Metrics["writingbench_overall"]) != 1 {
		t.Fatalf("generator used joined rather than source-native population: %#v", reference)
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
	if loss.Status != "drifted" {
		t.Fatalf("coverage loss did not produce drift: %#v", loss)
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
