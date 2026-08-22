package evals

import (
	"encoding/hex"
	"math"
	"sort"
	"strings"
)

type benchmarkEvidence struct {
	Benchmark           string            `json:"benchmark"`
	RawScore            *float64          `json:"raw_score,omitempty"`
	Percentile          *float64          `json:"percentile,omitempty"`
	SourceVersion       string            `json:"source_version,omitempty"`
	URL                 string            `json:"url,omitempty"`
	CommitSHA           string            `json:"commit_sha,omitempty"`
	ContentSHA          string            `json:"content_sha256,omitempty"`
	Methodology         string            `json:"methodology,omitempty"`
	JudgeVersion        string            `json:"judge_version,omitempty"`
	ReferencePopulation int               `json:"reference_population"`
	Family              string            `json:"family,omitempty"`
	NominalWeight       float64           `json:"nominal_weight,omitempty"`
	EvidenceMultiplier  float64           `json:"evidence_multiplier,omitempty"`
	EffectiveWeight     float64           `json:"effective_weight,omitempty"`
	EvidenceGrade       string            `json:"evidence_grade,omitempty"`
	SourceID            string            `json:"source_id,omitempty"`
	SourceRevision      string            `json:"source_revision,omitempty"`
	SourceClass         string            `json:"source_class,omitempty"`
	Mirrors             []BenchmarkMirror `json:"mirrors,omitempty"`
	IdentityMultiplier  float64           `json:"identity_multiplier,omitempty"`
	Stale               bool              `json:"stale,omitempty"`
	Conflict            string            `json:"conflict,omitempty"`
}

// LlamboScore is one category-specific score. A nil map value means its
// primary benchmark has no matching source result; it is never an average.
type LlamboScore struct {
	Score           float64             `json:"score"`
	Coverage        float64             `json:"coverage"`
	TrustedCoverage float64             `json:"trusted_coverage"`
	Contributions   []benchmarkEvidence `json:"contributions,omitempty"`
	Primary         *benchmarkEvidence  `json:"primary,omitempty"`
	Checks          []benchmarkEvidence `json:"checks"`
	Agreement       *float64            `json:"agreement"`
	Confidence      string              `json:"confidence"`
	Families        []string            `json:"families,omitempty"`
	WinnerStatus    string              `json:"winner_status,omitempty"`
	Stale           bool                `json:"stale,omitempty"`
	Conflicts       []string            `json:"conflicts,omitempty"`
	Estimated       bool                `json:"estimated,omitempty"`
	EstimateMethod  string              `json:"estimate_method,omitempty"`
	EstimateSources []string            `json:"estimate_source_categories,omitempty"`
}

// scoreCategory preserves the LLAMBO-2 scorer for rollback and frozen-reference
// diagnostics. New reports call scoreCategoryV3.
func scoreCategory(model Model, spec categorySpec, reference FormulaReference) *LlamboScore {
	legacy := legacyCategorySpec(spec.name)
	var selected benchmarkSpec
	var raw *float64
	var cohort []float64
	for index, candidate := range legacy.primaries {
		value, ok := candidate.value(model)
		values := reference.Metrics[candidate.reference]
		if !ok || value == nil || !finite(*value) || len(values) == 0 {
			continue
		}
		if index > 0 && !eligibleLaterPrimary(model, candidate, values) {
			continue
		}
		selected, raw, cohort = candidate, value, values
		break
	}
	if raw == nil {
		return nil
	}
	percentile := empiricalPercentile(cohort, *raw, true)
	primary := evidenceFor(model, selected, raw, percentile, len(cohort))
	checks := make([]benchmarkEvidence, 0, len(legacy.checks))
	deltas := make([]float64, 0, len(legacy.checks))
	for _, check := range legacy.checks {
		value, ok := check.value(model)
		values := reference.Metrics[check.reference]
		if check.benchmark == selected.benchmark || !ok || value == nil || !finite(*value) || len(values) == 0 {
			continue
		}
		checkPercentile := empiricalPercentile(values, *value, true)
		checks = append(checks, evidenceFor(model, check, value, checkPercentile, len(values)))
		deltas = append(deltas, math.Abs(checkPercentile-percentile))
	}
	var agreement *float64
	if len(deltas) > 0 {
		value := 100 - mean(deltas)
		agreement = &value
	}
	return &LlamboScore{Score: percentile, Coverage: 1, Primary: &primary, Contributions: []benchmarkEvidence{primary}, Checks: checks, Agreement: agreement, Confidence: categoryConfidence(primaryIdentity(model, selected), primaryStale(model, selected), agreement, len(checks))}
}

func legacyCategorySpec(name string) categorySpec {
	switch name {
	case "agents":
		return categorySpec{primaries: []benchmarkSpec{{"mcp-atlas", "llm_mcp_atlas", "llm_mcp_atlas", llmValue(func(v *LLMStatsMetrics) *float64 { return v.MCPAtlas })}, {"bfcl-v4", "bfcl_v4", "bfcl_v4", benchmarkValue("bfcl-v4")}, {"tau2-bench", "tau2_bench", "tau2_bench", benchmarkValue("tau2-bench")}, {"tau3-bench", "tau3_bench", "tau3_bench", benchmarkValue("tau3-bench")}, {"toolsandbox", "toolsandbox", "toolsandbox", benchmarkValue("toolsandbox")}}, checks: []benchmarkSpec{{"artificial-analysis-agentic-index", "aa_agentic_index", "aa_agentic_index", aaValue(func(v *ArtificialMetrics) *float64 { return v.Agentic })}}}
	case "coding":
		return categorySpec{primaries: []benchmarkSpec{{"swe-bench-verified", "llm_swe_bench_verified", "llm_swe_bench_verified", llmValue(func(v *LLMStatsMetrics) *float64 { return v.SWEVerified })}, {"swe-bench-pro", "llm_swe_bench_pro", "llm_swe_bench_pro", benchmarkValue("swe-bench-pro")}, {"scicode", "llm_scicode", "llm_scicode", benchmarkValue("scicode")}, {"livecodebench-v6", "livecodebench_v6", "livecodebench_v6", benchmarkValue("livecodebench-v6")}}, checks: []benchmarkSpec{{"swe-bench-pro", "llm_swe_bench_pro", "llm_swe_bench_pro", llmValue(func(v *LLMStatsMetrics) *float64 { return v.SWEPro })}, {"scicode", "llm_scicode", "llm_scicode", llmValue(func(v *LLMStatsMetrics) *float64 { return v.SciCode })}, {"artificial-analysis-coding-index", "aa_coding_index", "aa_coding_index", aaValue(func(v *ArtificialMetrics) *float64 { return v.Coding })}}}
	case "instruction-following":
		return categorySpec{primaries: []benchmarkSpec{{"instruction-following-index", "llm_instruction_general", "llm_instruction_general", llmIndexValue("instruction_following")}, {"ifeval-official", "ifeval_official", "ifeval_official", benchmarkValue("ifeval-official")}, {"ifbench", "ifbench", "ifbench", benchmarkValue("ifbench")}, {"multi-if", "multi_if", "multi_if", benchmarkValue("multi-if")}, {"ifstruct", "ifstruct", "ifstruct", benchmarkValue("ifstruct")}}, checks: []benchmarkSpec{{"writingbench-format", "writingbench_format", "writingbench_format", benchmarkDetailAverageValue("writingbench", "format_r", "format_c")}, {"writingbench-length", "writingbench_length", "writingbench_length", benchmarkDetailAverageValue("writingbench", "length_r", "length_c")}}}
	case "writing":
		return categorySpec{primaries: []benchmarkSpec{{"writingbench", "writingbench_overall", "writingbench_overall", benchmarkValue("writingbench")}}, checks: []benchmarkSpec{{"writingbench-domain", "writingbench_domain", "writingbench_domain", benchmarkDetailPrefixAverageValue("writingbench", "domain1_", "domain2_")}, {"writingbench-requirements", "writingbench_requirements", "writingbench_requirements", benchmarkDetailPrefixAverageValue("writingbench", "style_", "format_", "length_")}, {"eqbench-creative-v3", "eqbench_creative_v3", "eqbench_creative_v3", benchmarkValue("eqbench-creative-v3")}}}
	case "reasoning":
		return categorySpec{primaries: []benchmarkSpec{{"gpqa", "llm_gpqa", "llm_gpqa", llmValue(func(v *LLMStatsMetrics) *float64 { return v.GPQA })}, {"aime-versioned", "aime_versioned", "aime_versioned", benchmarkValue("aime-versioned")}}}
	case "long-context":
		return categorySpec{primaries: []benchmarkSpec{{"long-context-index", "llm_long_context_index", "llm_long_context_index", llmIndexValue("long_context")}, {"longbench-v2", "longbench_v2", "longbench_v2", benchmarkValue("longbench-v2")}, {"ruler", "ruler", "ruler", benchmarkValue("ruler")}, {"helmet", "helmet", "helmet", benchmarkValue("helmet")}}}
	default:
		return categorySpec{}
	}
}

func scoreCategoryV3(model Model, spec categorySpec, models []Model) *LlamboScore {
	_ = models // Reference cohorts are checked-in, never rebuilt from cache rows.
	contributions := make([]benchmarkEvidence, 0, len(spec.families))
	weighted, trustedWeight := 0.0, 0.0
	represented := make([]string, 0, len(spec.families))
	conflicts := make([]string, 0)
	stale := false
	for _, family := range spec.families {
		var selected *benchmarkEvidence
		for _, benchmark := range family.benchmarks {
			result, ok := model.Benchmarks[benchmark]
			if ok && result.Quarantined {
				conflicts = append(conflicts, benchmark+": "+result.Conflict)
				continue
			}
			if !ok || !eligibleCanonicalResult(result) {
				continue
			}
			cohort := resolvedFrozenBenchmarkCohort(benchmark, result)
			if len(cohort) < 5 {
				continue
			}
			higher := result.Direction != "lower"
			percentile := empiricalPercentile(cohort, *result.Score, higher)
			trust := evidenceMultiplier(result.EvidenceGrade) * identityMultiplier(result.Identity)
			evidence := evidenceForResult(benchmark, family.name, 1, result, percentile, len(cohort))
			evidence.EvidenceMultiplier, evidence.IdentityMultiplier, evidence.EffectiveWeight, evidence.Stale = evidenceMultiplier(result.EvidenceGrade), identityMultiplier(result.Identity), trust, result.Stale
			// A family contributes once. Its first reviewed benchmark in registry
			// order wins, so duplicates never amplify a model's score.
			selected = &evidence
			break
		}
		if selected == nil {
			continue
		}
		contributions = append(contributions, *selected)
		represented = append(represented, family.name)
		weighted += selected.EffectiveWeight * *selected.Percentile
		trustedWeight += selected.EffectiveWeight
		stale = stale || selected.Stale
	}
	if len(contributions) == 0 {
		return nil
	}
	sort.Slice(contributions, func(i, j int) bool { return contributions[i].Family < contributions[j].Family })
	primary := contributions[0]
	coverage := float64(len(represented)) / float64(len(spec.families))
	trustedCoverage := trustedWeight / float64(len(spec.families))
	// Sparse or low-trust evidence moves only partway from neutral 50. The
	// underlying benchmark percentile remains in Contributions for audit.
	raw := weighted / trustedWeight
	score := 50 + (raw-50)*trustedCoverage
	confidence := "low"
	if !stale && trustedCoverage >= .75 {
		confidence = "high"
	} else if !stale && trustedCoverage >= .50 {
		confidence = "medium"
	}
	sort.Strings(conflicts)
	return &LlamboScore{Score: score, Coverage: coverage, TrustedCoverage: trustedCoverage, Contributions: contributions, Primary: &primary, Confidence: confidence, Families: represented, Stale: stale, Conflicts: conflicts}
}

func eligibleCohort(models []Model, benchmark string) []float64 {
	return eligibleCohortRevision(models, benchmark, "")
}

func eligibleCohortRevision(models []Model, benchmark, version string) []float64 {
	values := make([]float64, 0, len(models))
	for _, model := range models {
		result, ok := model.Benchmarks[benchmark]
		if ok && eligibleCanonicalResult(result) && (version == "" || result.Version == version) {
			values = append(values, *result.Score)
		}
	}
	sort.Float64s(values)
	return values
}

func eligibleCanonicalResult(result BenchmarkResult) bool {
	if result.Score == nil || !finite(*result.Score) || result.Identity != IdentityMatchExact || !validSHA256(result.ContentSHA) || strings.TrimSpace(result.Version) == "" || strings.TrimSpace(result.Method) == "" {
		return false
	}
	if result.Direction != "" && result.Direction != "higher" && result.Direction != "lower" {
		return false
	}
	return evidenceMultiplier(result.EvidenceGrade) > 0 && registeredSourceEligible(result)
}

func identityMultiplier(identity IdentityMatch) float64 {
	if identity == IdentityMatchExact {
		return 1
	}
	return 0
}

func evidenceMultiplier(grade string) float64 {
	switch grade {
	case "owner", "benchmark_owner":
		return 1
	case "first_party", "official_submission":
		return .90
	case "official", "official_judgment":
		return .85
	case "aggregator_verified":
		return .80
	case "aggregator_self_reported":
		return .50
	case "", "aggregator_unspecified", "sealed_direct":
		return .35
	default:
		return 0
	}
}

func evidenceForResult(benchmark, family string, weight float64, result BenchmarkResult, percentile float64, population int) benchmarkEvidence {
	grade := result.EvidenceGrade
	if grade == "" {
		grade = "sealed_direct"
	}
	return benchmarkEvidence{Benchmark: benchmark, RawScore: cloneFloat64(result.Score), Percentile: scoreFloat64Ptr(percentile), SourceVersion: result.Version, URL: result.URL, CommitSHA: result.CommitSHA, ContentSHA: result.ContentSHA, Methodology: result.Method, JudgeVersion: result.Judge, ReferencePopulation: population, Family: family, NominalWeight: weight, EvidenceGrade: grade, SourceID: result.SourceID, SourceRevision: result.SourceRevision, SourceClass: result.SourceClass, Mirrors: append([]BenchmarkMirror(nil), result.Mirrors...)}
}

// Later authority-ladder sources are unsealed until the model result and its
// independent cohort are both reproducible. LLAMBO-2's first primaries retain
// their historical source adapters for compatibility with the sealed reference.
func eligibleLaterPrimary(model Model, spec benchmarkSpec, cohort []float64) bool {
	if len(cohort) < 5 {
		return false
	}
	result, ok := model.Benchmarks[spec.benchmark]
	if !ok || result.Identity != IdentityMatchExact || result.Score == nil || !finite(*result.Score) {
		return false
	}
	if strings.TrimSpace(result.Version) == "" || strings.TrimSpace(result.Method) == "" {
		return false
	}
	return validSHA256(result.ContentSHA)
}

func validSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func primaryStale(model Model, spec benchmarkSpec) bool {
	if result, ok := model.Benchmarks[spec.benchmark]; ok {
		return result.Stale
	}
	return model.EvidenceStale
}

func primaryIdentity(model Model, spec benchmarkSpec) IdentityMatch {
	if result, ok := model.Benchmarks[spec.benchmark]; ok && result.Identity != "" {
		return result.Identity
	}
	// LLM Stats owns its source-native benchmark and index rows. A missing AA
	// cross-source join does not make that primary identity ambiguous.
	if strings.HasPrefix(spec.name, "llm_") {
		return IdentityMatchExact
	}
	return model.IdentityMatch
}

func evidenceFor(model Model, spec benchmarkSpec, raw *float64, percentile float64, referencePopulation int) benchmarkEvidence {
	evidence := benchmarkEvidence{Benchmark: spec.benchmark, RawScore: cloneFloat64(raw), Percentile: scoreFloat64Ptr(percentile), ReferencePopulation: referencePopulation}
	result, ok := model.Benchmarks[spec.benchmark]
	if !ok && strings.HasPrefix(spec.benchmark, "writingbench-") {
		result, ok = model.Benchmarks["writingbench"]
	}
	if ok {
		evidence.SourceVersion, evidence.URL, evidence.CommitSHA, evidence.ContentSHA, evidence.Methodology, evidence.JudgeVersion = result.Version, result.URL, result.CommitSHA, result.ContentSHA, result.Method, result.Judge
	}
	return evidence
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func scoreFloat64Ptr(value float64) *float64 { return &value }

func categoryConfidence(identity IdentityMatch, stale bool, agreement *float64, checks int) string {
	if stale {
		return "low"
	}
	if identity == IdentityMatchProjected || identity == IdentityMatchAmbiguous || identity == IdentityMatchUnmatched {
		return "low"
	}
	if agreement != nil && checks >= 2 && *agreement >= 90 && identity == IdentityMatchExact {
		return "high"
	}
	if agreement != nil && *agreement >= 80 {
		return "medium"
	}
	if checks == 0 && (identity == IdentityMatchExact || identity == IdentityMatchNormalized) {
		return "medium"
	}
	return "low"
}

func operationalScore(model Model, dimension string, reference FormulaReference) *ExternalScore {
	values := make([]float64, 0, 2)
	for _, metric := range operationalMetrics {
		if metric.dimension != dimension {
			continue
		}
		raw, ok := metric.value(model)
		if !ok || raw == nil || len(reference.Metrics[metric.name]) == 0 {
			continue
		}
		values = append(values, empiricalPercentile(reference.Metrics[metric.name], *raw, metric.higher))
	}
	if len(values) == 0 {
		return nil
	}
	return &ExternalScore{Score: mean(values), RawScore: mean(values), Low: mean(values), High: mean(values), Coverage: 1, Confidence: "medium", Signals: len(values)}
}

func empiricalPercentile(sortedValues []float64, value float64, higher bool) float64 {
	if len(sortedValues) == 0 {
		return 0
	}
	left := sort.SearchFloat64s(sortedValues, value)
	right := sort.Search(len(sortedValues), func(i int) bool { return sortedValues[i] > value })
	percentile := 100 * (float64(left) + .5*float64(right-left)) / float64(len(sortedValues))
	if !higher {
		percentile = 100 - percentile
	}
	return clamp(percentile)
}
