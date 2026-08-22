package evals

import (
	"math"
	"strings"
	"testing"
)

func TestLLAMBO2OrderedPrimaryFallback(t *testing.T) {
	low, high := 10.0, 90.0
	models := []Model{{Key: "low", Name: "low", IdentityMatch: IdentityMatchExact, LLMStats: &LLMStatsMetrics{SWEVerified: &low, SWEPro: &high, Indexes: map[string]Index{}}}, {Key: "missing", Name: "missing", IdentityMatch: IdentityMatchExact, Benchmarks: map[string]BenchmarkResult{"swe-bench-pro": sealedBenchmark(high)}}}
	reference := FormulaReference{Metrics: map[string][]float64{"llm_swe_bench_verified": {0, 50, 100}, "llm_swe_bench_pro": {0, 25, 50, 75, 100}, "llm_code_index": {0, 50, 100}, "aa_coding_index": {0, 50, 100}}}
	score := scoreCategory(models[0], categorySpecs[1], reference)
	if score == nil || score.Score != empiricalPercentile(reference.Metrics["llm_swe_bench_verified"], low, true) {
		t.Fatalf("primary did not exclusively own score: %#v", score)
	}
	fallback := scoreCategory(models[1], categorySpecs[1], reference)
	if fallback == nil || fallback.Primary == nil || fallback.Primary.Benchmark != "swe-bench-pro" {
		t.Fatalf("missing first source did not use the next eligible primary: %#v", fallback)
	}
}

func TestOrderedPrimaryRequiresIndependentCohort(t *testing.T) {
	value := 80.0
	model := Model{Benchmarks: map[string]BenchmarkResult{"bfcl-v4": sealedBenchmark(value), "tau2-bench": sealedBenchmark(value)}}
	spec := categorySpecs[0]
	noCohort := FormulaReference{Metrics: map[string][]float64{"bfcl_v4": nil, "tau2_bench": {10, 40, 60, 80, 90}}}
	score := scoreCategory(model, spec, noCohort)
	if score == nil || score.Primary == nil || score.Primary.Benchmark != "tau2-bench" || score.Primary.ReferencePopulation != 5 {
		t.Fatalf("empty earlier cohort should yield to independent later cohort: %#v", score)
	}
	model.Benchmarks = map[string]BenchmarkResult{"bfcl-v4": sealedBenchmark(value)}
	if scoreCategory(model, spec, FormulaReference{Metrics: map[string][]float64{"bfcl_v4": nil}}) != nil {
		t.Fatal("source value without a source-native cohort became a score")
	}
}

func TestOrderedPrimaryRejectsUnsealedEvidence(t *testing.T) {
	value := 80.0
	cohort := []float64{10, 40, 60, 80, 90}
	for _, test := range []struct {
		name   string
		result BenchmarkResult
		cohort []float64
	}{
		{"ambiguous identity", BenchmarkResult{Score: &value, Identity: IdentityMatchAmbiguous, Version: "v1", ContentSHA: strings.Repeat("a", 64), Method: "official"}, cohort},
		{"missing version", BenchmarkResult{Score: &value, Identity: IdentityMatchExact, ContentSHA: strings.Repeat("a", 64), Method: "official"}, cohort},
		{"missing method", BenchmarkResult{Score: &value, Identity: IdentityMatchExact, Version: "v1", ContentSHA: strings.Repeat("a", 64)}, cohort},
		{"invalid hash", BenchmarkResult{Score: &value, Identity: IdentityMatchExact, Version: "v1", ContentSHA: "not-a-sha", Method: "official"}, cohort},
		{"nonfinite score", BenchmarkResult{Score: float64Ptr(math.NaN()), Identity: IdentityMatchExact, Version: "v1", ContentSHA: strings.Repeat("a", 64), Method: "official"}, cohort},
		{"cohort too small", sealedBenchmark(value), cohort[:4]},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := Model{Benchmarks: map[string]BenchmarkResult{"bfcl-v4": test.result}}
			if score := scoreCategory(model, categorySpecs[0], FormulaReference{Metrics: map[string][]float64{"bfcl_v4": test.cohort}}); score != nil {
				t.Fatalf("unsealed later primary became a score: %#v", score)
			}
		})
	}
}

func sealedBenchmark(value float64) BenchmarkResult {
	return BenchmarkResult{Score: &value, Identity: IdentityMatchExact, Version: "v1", ContentSHA: strings.Repeat("a", 64), Method: "official methodology", SourceID: "swe-bench", SourceClass: string(SourceOwnerResult), EvidenceGrade: "owner", SourceRevision: "v1"}
}

func TestLLAMBO1UsesPrimarySourceIdentityForConfidence(t *testing.T) {
	primary, checkA, checkB := 90.0, 90.0, 90.0
	model := Model{IdentityMatch: IdentityMatchUnmatched, LLMStats: &LLMStatsMetrics{
		SWEVerified: &primary, SWEPro: &checkA, SciCode: &checkB, Indexes: map[string]Index{},
	}, AA: &ArtificialMetrics{Coding: &checkB}}
	reference := FormulaReference{Metrics: map[string][]float64{
		"llm_swe_bench_verified": {0, 90, 100}, "llm_swe_bench_pro": {0, 90, 100},
		"llm_scicode": {0, 90, 100}, "aa_coding_index": {0, 90, 100},
	}}
	score := scoreCategory(model, categorySpecs[1], reference)
	if score == nil || score.Confidence != "high" {
		t.Fatalf("source-native exact primary was not high confidence: %#v", score)
	}
}

func TestLLAMBO1WritingUsesOfficialPrimaryAndEQOnlyAsCheck(t *testing.T) {
	primary, corroborator := 78.0, 1800.0
	model := Model{Key: "writer", Name: "Writer", Benchmarks: map[string]BenchmarkResult{
		"writingbench":        {Score: &primary, Identity: IdentityMatchExact, Version: "wb-v1", ContentSHA: strings.Repeat("a", 64), Judge: "Claude-Sonnet-4-5"},
		"eqbench-creative-v3": {Score: &corroborator, Identity: IdentityMatchNormalized, CommitSHA: EQBenchCreativeCommit, Judge: "Claude Sonnet 4.6"},
	}}
	reference := FormulaReference{Metrics: map[string][]float64{
		"writingbench_overall": {50, 75, 80}, "eqbench_creative_v3": {1000, 1700, 1900},
	}}
	score := scoreCategory(model, categorySpecs[len(categorySpecs)-1], reference)
	if score == nil || score.Score != empiricalPercentile(reference.Metrics["writingbench_overall"], primary, true) || len(score.Checks) != 1 {
		t.Fatalf("writing primary/check contract failed: %#v", score)
	}
	if score.Primary == nil || score.Primary.SourceVersion != "wb-v1" || score.Primary.JudgeVersion == "" || score.Checks[0].CommitSHA != EQBenchCreativeCommit {
		t.Fatalf("writing provenance missing: %#v", score)
	}
}

func TestLLAMBO1WritingIncludesWritingBenchDomainAndRequirementChecks(t *testing.T) {
	primary := 78.0
	model := Model{Benchmarks: map[string]BenchmarkResult{"writingbench": {
		Score: &primary, Identity: IdentityMatchExact, Details: map[string]float64{
			"domain1_academic": 80, "domain2_report": 60, "style_r": 70, "format_c": 50, "length_r": 60,
		},
	}}}
	reference := FormulaReference{Metrics: map[string][]float64{
		"writingbench_overall": {50, 78, 90}, "writingbench_domain": {50, 70, 90},
		"writingbench_requirements": {40, 60, 80},
	}}
	score := scoreCategory(model, categorySpecs[len(categorySpecs)-1], reference)
	if score == nil || len(score.Checks) != 2 || score.Checks[0].Benchmark != "writingbench-domain" || score.Checks[1].Benchmark != "writingbench-requirements" {
		t.Fatalf("WritingBench domain/requirement checks missing: %#v", score)
	}
}

func TestLLAMBO1InstructionFollowingUsesWritingBenchRequirementChecks(t *testing.T) {
	primary := 50.0
	model := Model{LLMStats: &LLMStatsMetrics{Indexes: map[string]Index{
		"instruction_following": {Conservative: primary},
	}}, Benchmarks: map[string]BenchmarkResult{"writingbench": {
		Identity: IdentityMatchExact, Version: "wb-v1", Details: map[string]float64{
			"format_r": 80, "format_c": 60, "length_r": 70, "length_c": 50,
		},
	}}}
	reference := FormulaReference{Metrics: map[string][]float64{
		"llm_instruction_general": {0, 50, 100}, "llm_structured_reasoning": {0, 50, 100},
		"writingbench_format": {0, 70, 100}, "writingbench_length": {0, 60, 100},
	}}
	score := scoreCategory(model, categorySpecs[2], reference)
	if score == nil || len(score.Checks) != 2 || score.Checks[0].Benchmark != "writingbench-format" || score.Checks[1].Benchmark != "writingbench-length" {
		t.Fatalf("WritingBench requirement checks missing: %#v", score)
	}
	if score.Checks[0].SourceVersion != "wb-v1" || *score.Checks[0].RawScore != 70 || *score.Checks[1].RawScore != 60 {
		t.Fatalf("WritingBench requirement evidence incorrect: %#v", score.Checks)
	}
}

func TestLLAMBO2InstructionFollowingFallsBackToOfficialIFEval(t *testing.T) {
	ifeval := 91.84
	model := Model{Benchmarks: map[string]BenchmarkResult{"ifeval-official": {
		Score: &ifeval, Identity: IdentityMatchExact, Version: "commit", ContentSHA: strings.Repeat("a", 64), Method: "official methodology",
	}}}
	reference := FormulaReference{Metrics: map[string][]float64{
		"llm_instruction_general": {0, 50, 100}, "ifeval_official": {75, 82.23, 87.8, 90, 91.84},
	}}
	score := scoreCategory(model, categorySpecs[2], reference)
	if score == nil || score.Primary == nil || score.Primary.Benchmark != "ifeval-official" || score.Score != 90 || score.Confidence != "medium" {
		t.Fatalf("official IFEval fallback failed: %#v", score)
	}
}

func TestEmpiricalPercentileUsesMidranks(t *testing.T) {
	if got := empiricalPercentile([]float64{1, 2, 2, 3}, 2, true); got != 50 {
		t.Fatalf("got %v, want 50", got)
	}
}

func TestLLAMBO1AgreementAndConfidenceThresholds(t *testing.T) {
	primary, checkA, checkB := 50.0, 60.0, 40.0
	model := Model{LLMStats: &LLMStatsMetrics{
		SWEVerified: &primary, SWEPro: &checkA, SciCode: &checkB, Indexes: map[string]Index{},
	}}
	reference := FormulaReference{Metrics: map[string][]float64{
		"llm_swe_bench_verified": {0, 50, 100}, "llm_swe_bench_pro": {0, 50, 100},
		"llm_scicode": {0, 50, 100}, "aa_coding_index": {0, 50, 100},
	}}
	score := scoreCategory(model, categorySpecs[1], reference)
	wantAgreement := 100 - (math.Abs(66.66666666666667-50)+math.Abs(33.333333333333336-50))/2
	if score == nil || score.Agreement == nil || math.Abs(*score.Agreement-wantAgreement) > 1e-9 || score.Confidence != "medium" {
		t.Fatalf("unexpected agreement/confidence: %#v", score)
	}

	for _, test := range []struct {
		name      string
		identity  IdentityMatch
		stale     bool
		agreement *float64
		checks    int
		want      string
	}{
		{"exact high boundary", IdentityMatchExact, false, float64Ptr(90), 2, "high"},
		{"normalized agreement", IdentityMatchNormalized, false, float64Ptr(80), 1, "medium"},
		{"exact sparse", IdentityMatchExact, false, nil, 0, "medium"},
		{"below agreement", IdentityMatchExact, false, float64Ptr(79.9), 1, "low"},
		{"stale", IdentityMatchExact, true, float64Ptr(100), 2, "low"},
		{"ambiguous", IdentityMatchAmbiguous, false, float64Ptr(100), 2, "low"},
		{"projected", IdentityMatchProjected, false, float64Ptr(100), 2, "low"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := categoryConfidence(test.identity, test.stale, test.agreement, test.checks); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestLLAMBO1RenderMatrix(t *testing.T) {
	primary := &benchmarkEvidence{Benchmark: "swe-bench-verified"}
	report := Report{FormulaVersion: FormulaVersion, RankingProfile: "coding", Models: []ReportModel{{Name: "model", LlamboScores: map[string]*LlamboScore{"coding": {Score: 50, Primary: primary, Confidence: "medium"}}, Scores: map[string]*ExternalScore{}}}}
	for _, want := range []string{"Llambo Score Matrix", "Instruction Following", "swe-bench-verified"} {
		if !strings.Contains(RenderMarkdown(report, 0), want) {
			t.Fatalf("missing %q", want)
		}
	}
	markdown := RenderMarkdown(report, 0)
	positions := []int{strings.Index(markdown, "| Agents |"), strings.Index(markdown, "| Coding |"), strings.Index(markdown, "| Instruction Following |"), strings.Index(markdown, "| Long Context |"), strings.Index(markdown, "| Reasoning |"), strings.Index(markdown, "| Writing |")}
	for i := 1; i < len(positions); i++ {
		if positions[i-1] < 0 || positions[i] <= positions[i-1] {
			t.Fatalf("matrix categories are not alphabetically ordered: %v", positions)
		}
	}
}

func TestLLAMBO1RejectsRemovedRankingProfiles(t *testing.T) {
	for _, profile := range []string{"overall", "general", "value"} {
		if _, err := BuildReport(Result{}, profile); err == nil || !strings.Contains(err.Error(), "unsupported ranking profile") {
			t.Fatalf("removed profile %q accepted: %v", profile, err)
		}
	}
}

func externalTestModel(key string, intelligence, coding, agentic, general, code float64) Model {
	return Model{Key: key, Name: key, IdentityMatch: IdentityMatchExact, Benchmarks: map[string]BenchmarkResult{"swe-bench-verified": sealedBenchmark(code)}, AA: &ArtificialMetrics{Intelligence: float64Ptr(intelligence), Coding: float64Ptr(coding), Agentic: float64Ptr(agentic)}, LLMStats: &LLMStatsMetrics{GPQA: float64Ptr(general / 100), SWEVerified: float64Ptr(code / 100), SWEPro: float64Ptr(code / 100), MCPAtlas: float64Ptr(agentic), Indexes: map[string]Index{"general": {Conservative: general}, "reasoning": {Conservative: general}, "instruction_following": {Conservative: general}, "factuality": {Conservative: general}, "code": {Conservative: code}, "agents": {Conservative: general}, "tool_calling": {Conservative: general}, "structured_output": {Conservative: general}, "writing": {Conservative: general}, "long_context": {Conservative: general}, "grounding": {Conservative: general}}}}
}
func float64Ptr(value float64) *float64 { return &value }
func closeTo(t *testing.T, got, want float64) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}
