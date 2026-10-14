package evals

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Repeated string literals consolidated by goconst.
const (
	artificialAnalysisKey = "artificial_analysis"
	eqbenchCreativeV3Key  = "eqbench_creative_v3"
	higher1               = "higher"
	ifevalOfficial        = "ifeval-official"
	ifevalOfficialKey     = "ifeval_official"
	llmStatsKey           = "llm_stats"
	writingbench1         = "writingbench"
)

const writingbenchOverall = "writingbench_overall"

const publicLeaderboard = "public-leaderboard"

// les-v1-reference.json is the checked-in frozen input distribution. Its
// filename is retained for cache compatibility; the embedded version is
// LLAMBO-2 and changing it requires a new formula version.
// go run ./internal/evals/cmd/generate-reference
//
//go:embed testdata/les-v1-reference.json
var formulaReferenceJSON []byte

// llmStatsFrozenCohortsJSON preserves the six approved LLM Stats populations
// extracted from the SHA-pinned sealed observation artifact. SQLite remains an
// index of that artifact; it does not own or rebuild these percentile scales.
//
//go:embed testdata/llm-stats-frozen-cohorts-v1.json
var llmStatsFrozenCohortsJSON []byte

// llmStatsStatsV1FrozenCohortsJSON freezes the eligible reviewed benchmark
// populations collected from the authenticated Stats v1 API. These populations
// own LLAMBO-8 percentile scales; mutable API membership cannot move an
// installed score.
//
//go:embed testdata/llm-stats-stats-v1-frozen-cohorts-v1.json
var llmStatsStatsV1FrozenCohortsJSON []byte

// formulaReferenceDigest pins the exact, versioned LLAMBO-2 cohort. Source
// refreshes must generate and review a new formula version rather than silently
// changing the percentile population used by an installed binary.
const formulaReferenceDigest = "74d5049896f5c8de4c01d22d15357e5451209d215b7e6410b9169dc6cb30ffed"

const llmStatsFrozenCohortsArtifactDigest = "de5462f48f2ac7d36713cdf96b57d63832588e598d0bae54e02f7c47737a4f8a"
const llmStatsFrozenCohortsDigest = "0e47a655d96911e5f8db8ed83b645adf352d66b4dda803813e91ab1ff92ff8bb"
const llmStatsStatsV1FrozenCohortsArtifactDigest = "540bdcdff3b2eae7c816d993950789f15ca1685a7ba09ff679a056dc05478583"
const llmStatsStatsV1FrozenCohortsDigest = "5fe1b346fcd324bc2401c756a5df1b7b398b4ce326709353e8b889fa09e7331a"

var frozenReferenceMetrics = []string{
	"aa_agentic_coding", "aa_agentic_general", aaAgenticIndex, "aa_agentic_reasoning",
	"aa_coding_agents", "aa_coding_general", aaCodingIndex, "aa_coding_reasoning",
	"aa_end_to_end_time", "aa_input_price", "aa_intelligence_agents", "aa_intelligence_coding",
	"aa_intelligence_general", "aa_intelligence_reasoning", "aa_output_price", "aa_output_speed",
	"aa_time_to_first_token", eqbenchCreativeV3Key, ifevalOfficialKey, "llm_agents_index",
	"llm_code_index", "llm_communication_index", "llm_creativity_writing", "llm_factuality_general",
	"llm_factuality_long", "llm_general_index", "llm_general_reasoning", llmGpqa, "llm_grounding_long",
	"llm_input_price", llmInstructionGeneral, "llm_instruction_long", "llm_instruction_reasoning",
	"llm_instruction_writing", "llm_language_writing", llmLongContextIndex, llmMcpAtlas,
	"llm_output_price", "llm_output_speed", "llm_reasoning_agents", "llm_reasoning_general",
	"llm_reasoning_index", llmScicode, "llm_structured_agents", "llm_structured_reasoning",
	llmSweBenchPro, llmSweBenchVerified, "llm_tool_calling_index", "llm_writing_index",
	writingbenchDomain, writingbenchFormat, writingbenchLength, writingbenchOverall, writingbenchRequirements,
}

// FormulaReference freezes the reference population behind percentile
// scoring.
type FormulaReference struct {
	FormulaVersion     string               `json:"formula_version"`
	CreatedAt          time.Time            `json:"created_at"`
	AAVersion          float64              `json:"artificial_analysis_index_version"`
	SourceModelCounts  map[string]int       `json:"source_model_counts"`
	SourceFingerprints map[string]string    `json:"source_fingerprints"`
	Metrics            map[string][]float64 `json:"metrics"`
}

type frozenLLMStatsCohorts struct {
	SchemaVersion        string                          `json:"schema_version"`
	SealedArtifactSHA256 string                          `json:"sealed_artifact_sha256"`
	Benchmarks           map[string]frozenLLMStatsCohort `json:"benchmarks"`
}

type frozenLLMStatsCohort struct {
	SourceID         string    `json:"source_id"`
	SourceClass      string    `json:"source_class"`
	BenchmarkVersion string    `json:"benchmark_version"`
	Cohort           string    `json:"cohort"`
	Methodology      string    `json:"methodology"`
	Direction        string    `json:"direction"`
	MinPopulation    int       `json:"min_population"`
	RowCount         int       `json:"row_count"`
	Digests          []string  `json:"digests"`
	Scores           []float64 `json:"scores"`
}

// scoringContext owns the immutable reference data used by one report build.
// Loading it once makes corrupt checked-in scoring inputs a report error and
// avoids repeatedly decoding the same sealed cohorts in the category loops.
type scoringContext struct {
	reference      FormulaReference
	legacyCohorts  frozenLLMStatsCohorts
	statsV1Cohorts frozenLLMStatsCohorts
}

func normalizedFrozenRevision(benchmark string, result BenchmarkResult) string {
	version := strings.TrimSpace(result.Version)
	switch benchmark {
	case writingbench1:
		return version
	case ifevalOfficial, eqbenchCreativeV3:
		return strings.TrimSpace(result.CommitSHA)
	}
	return version
}

func frozenBenchmarkCohort(benchmark, revision string) []float64 {
	reference, err := loadFormulaReference()
	if err != nil {
		return nil
	}
	return frozenBenchmarkCohortFromReference(benchmark, revision, reference)
}

func frozenBenchmarkCohortFromReference(benchmark, revision string, reference FormulaReference) []float64 {
	if cohort := officialCardFrozenCohorts[benchmark][revision]; len(cohort) != 0 {
		clone := append([]float64(nil), cohort...)
		sort.Float64s(clone)
		return clone
	}
	metric, ok := frozenBenchmarkReference[benchmark][revision]
	if !ok {
		return nil
	}
	values := reference.Metrics[metric]
	if len(values) == 0 {
		return nil
	}
	return append([]float64(nil), values...)
}

// resolvedFrozenBenchmarkCohort is the scorer boundary for immutable benchmark
// populations. LLAMBO-8 prefers a broad common Stats v1 population; older
// source-specific cohorts remain fallbacks for benchmarks without one.
func resolvedFrozenBenchmarkCohort(benchmark string, result BenchmarkResult) []float64 {
	context, err := loadScoringContext()
	if err != nil {
		return nil
	}
	return context.resolvedFrozenBenchmarkCohort(benchmark, result)
}

func (context scoringContext) resolvedFrozenBenchmarkCohort(benchmark string, result BenchmarkResult) []float64 {
	// LLAMBO-8's reviewed LFM2.5-8B-A1B card cohorts are same-methodology
	// populations and therefore own this exact revision's scale. This narrow
	// exception does not change LLAMBO-7 arithmetic or other source precedence.
	if result.SourceID == lfm25A1BSourceID && result.SourceRevision == lfm25A1BRevision {
		if cohort := officialCardFrozenCohorts[benchmark][lfm25A1BRevision]; len(cohort) != 0 {
			clone := append([]float64(nil), cohort...)
			sort.Float64s(clone)
			return clone
		}
	}
	// LLAMBO-8 uses the broad Stats v1 population as the common percentile scale
	// whenever one exists. Source authority may choose a raw result, but it must
	// not replace that scale with a tiny source-native comparison table.
	if cohort, ok := context.statsV1Cohorts.Benchmarks[benchmark]; ok {
		if result.Direction != "" && result.Direction != cohort.Direction {
			return nil
		}
		return append([]float64(nil), cohort.Scores...)
	}
	if result.SourceID == llmStatsStatsV1SourceID {
		return nil
	}
	if _, targeted := frozenLLMStatsCohortPopulations[benchmark]; !targeted {
		return frozenBenchmarkCohortFromReference(benchmark, normalizedFrozenRevision(benchmark, result), context.reference)
	}
	cohort, ok := context.legacyCohorts.Benchmarks[benchmark]
	if !ok || !matchesFrozenLLMStatsCohort(result, cohort) {
		return nil
	}
	return append([]float64(nil), cohort.Scores...)
}

func loadFrozenLLMStatsCohorts() (frozenLLMStatsCohorts, error) {
	if got := sha256Hex(llmStatsFrozenCohortsJSON); got != llmStatsFrozenCohortsDigest {
		return frozenLLMStatsCohorts{}, fmt.Errorf("frozen LLM Stats cohort digest %s does not match pinned %s", got, llmStatsFrozenCohortsDigest)
	}
	var cohorts frozenLLMStatsCohorts
	if err := json.Unmarshal(llmStatsFrozenCohortsJSON, &cohorts); err != nil {
		return frozenLLMStatsCohorts{}, fmt.Errorf("decode frozen LLM Stats cohorts: %w", err)
	}
	if err := validateFrozenLLMStatsCohorts(cohorts); err != nil {
		return frozenLLMStatsCohorts{}, err
	}
	return cohorts, nil
}

func matchesFrozenLLMStatsCohort(result BenchmarkResult, cohort frozenLLMStatsCohort) bool {
	if result.Identity != IdentityMatchExact || result.SourceID != cohort.SourceID || result.SourceClass != cohort.SourceClass || result.Version != cohort.BenchmarkVersion || result.Cohort != cohort.Cohort || result.Method != cohort.Methodology || result.Direction != cohort.Direction || result.SourceRevision == "" || result.SourceRevision != result.ContentSHA {
		return false
	}
	for _, digest := range cohort.Digests {
		if result.SourceRevision == digest {
			return true
		}
	}
	return false
}

// frozenLLMStatsCohortAdmissionReason explains a target-specific rejection at
// the report boundary. Non-target benchmarks deliberately retain the generic
// unresolved reason so this contract does not broaden source admission.
func frozenLLMStatsCohortAdmissionReason(benchmark string, result BenchmarkResult) string {
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		return "frozen LLM Stats cohort is unavailable"
	}
	return frozenLLMStatsCohortAdmissionReasonFromCohorts(benchmark, result, cohorts)
}

func frozenLLMStatsCohortAdmissionReasonFromCohorts(benchmark string, result BenchmarkResult, cohorts frozenLLMStatsCohorts) string {
	if _, targeted := frozenLLMStatsCohortPopulations[benchmark]; !targeted {
		return ""
	}
	cohort, ok := cohorts.Benchmarks[benchmark]
	if !ok {
		return "frozen LLM Stats cohort is unavailable"
	}
	if result.Identity != IdentityMatchExact {
		return "frozen LLM Stats cohort requires exact identity"
	}
	if result.SourceID != cohort.SourceID || result.SourceClass != cohort.SourceClass {
		return "frozen LLM Stats cohort requires the reviewed source ID and class"
	}
	if result.Version != cohort.BenchmarkVersion {
		return "frozen LLM Stats cohort requires the pinned benchmark version"
	}
	if result.Cohort != cohort.Cohort {
		return "frozen LLM Stats cohort requires the pinned cohort"
	}
	if result.Method != cohort.Methodology {
		return "frozen LLM Stats cohort requires the pinned methodology"
	}
	if result.Direction != cohort.Direction {
		return "frozen LLM Stats cohort requires the pinned direction"
	}
	if result.SourceRevision == "" || result.SourceRevision != result.ContentSHA {
		return "frozen LLM Stats cohort requires matching source revision and content SHA"
	}
	for _, digest := range cohort.Digests {
		if result.SourceRevision == digest {
			return ""
		}
	}
	return "frozen LLM Stats cohort source revision is outside the pinned digest set"
}

func frozenLLMStatsStatsV1CohortAdmissionReasonFromCohorts(benchmark string, result BenchmarkResult, cohorts frozenLLMStatsCohorts) string {
	if result.SourceID != llmStatsStatsV1SourceID {
		return ""
	}
	cohort, ok := cohorts.Benchmarks[benchmark]
	if !ok {
		return "frozen Stats v1 cohort is unavailable for this benchmark"
	}
	if result.Identity != IdentityMatchExact {
		return "frozen Stats v1 cohort requires exact identity"
	}
	if result.SourceClass != cohort.SourceClass {
		return "frozen Stats v1 cohort requires the reviewed source class"
	}
	if result.Direction != "" && result.Direction != cohort.Direction {
		return "frozen Stats v1 cohort requires the pinned direction"
	}
	return ""
}

func frozenBenchmarkNames() []string {
	set := make(map[string]struct{}, len(frozenBenchmarkReference)+len(officialCardFrozenCohorts)+len(frozenLLMStatsCohortPopulations))
	for benchmark := range frozenBenchmarkReference {
		set[benchmark] = struct{}{}
	}
	for benchmark := range officialCardFrozenCohorts {
		set[benchmark] = struct{}{}
	}
	for benchmark := range frozenLLMStatsCohortPopulations {
		set[benchmark] = struct{}{}
	}
	if cohorts, err := loadFrozenLLMStatsStatsV1Cohorts(); err == nil {
		for benchmark := range cohorts.Benchmarks {
			set[benchmark] = struct{}{}
		}
	}
	names := make([]string, 0, len(set))
	for benchmark := range set {
		names = append(names, benchmark)
	}
	sort.Strings(names)
	return names
}

// FormulaReferenceSummary digests the reference for report diagnostics.
type FormulaReferenceSummary struct {
	CreatedAt          time.Time         `json:"created_at"`
	AAVersion          float64           `json:"artificial_analysis_index_version"`
	SourceModelCounts  map[string]int    `json:"source_model_counts"`
	SourceFingerprints map[string]string `json:"source_fingerprints"`
}

// DriftDiagnostics reports drift between current and reference sources.
type DriftDiagnostics struct {
	Status              string            `json:"status"`
	Reasons             []string          `json:"reasons,omitempty"`
	MaxKS               float64           `json:"max_ks"`
	WorstMetric         string            `json:"worst_metric,omitempty"`
	CurrentFingerprints map[string]string `json:"current_source_fingerprints"`
}

func loadFormulaReference() (FormulaReference, error) {
	if got := sha256Hex(formulaReferenceJSON); got != formulaReferenceDigest {
		return FormulaReference{}, fmt.Errorf("formula reference digest %s does not match pinned %s", got, formulaReferenceDigest)
	}
	var reference FormulaReference
	if err := json.Unmarshal(formulaReferenceJSON, &reference); err != nil {
		return FormulaReference{}, fmt.Errorf("decode formula reference: %w", err)
	}
	if reference.FormulaVersion != "LLAMBO-2" || len(reference.Metrics) == 0 {
		return FormulaReference{}, errors.New("formula reference is missing or does not match LLAMBO-2 rollback diagnostics")
	}
	if err := validateFormulaReference(reference); err != nil {
		return FormulaReference{}, err
	}
	return reference, nil
}

func validateFormulaReference(reference FormulaReference) error {
	if len(reference.Metrics) != len(frozenReferenceMetrics) {
		return fmt.Errorf("formula reference has %d metrics, want exact inventory of %d", len(reference.Metrics), len(frozenReferenceMetrics))
	}
	for _, name := range frozenReferenceMetrics {
		values := reference.Metrics[name]
		if len(values) == 0 {
			return fmt.Errorf("formula reference metric %s is missing", name)
		}
		for i, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("formula reference metric %s contains a non-finite value", name)
			}
			if i > 0 && values[i-1] > value {
				return fmt.Errorf("formula reference metric %s is not sorted", name)
			}
		}
	}
	for name := range reference.Metrics {
		if !containsFrozenReferenceMetric(name) {
			return fmt.Errorf("formula reference has unexpected metric %s", name)
		}
	}
	return nil
}

func referenceMetricNames() []string {
	return append([]string(nil), frozenReferenceMetrics...)
}

func containsFrozenReferenceMetric(name string) bool {
	index := sort.SearchStrings(frozenReferenceMetrics, name)
	return index < len(frozenReferenceMetrics) && frozenReferenceMetrics[index] == name
}

// GenerateFormulaReference renders the frozen reference population for the
// fetch result.
func GenerateFormulaReference(result Result) ([]byte, error) {
	createdAt := time.Time{}
	for _, source := range result.Sources {
		if source.FetchedAt.After(createdAt) {
			createdAt = source.FetchedAt
		}
	}
	if createdAt.IsZero() {
		createdAt = result.GeneratedAt
	}
	referenceModels := result.ReferenceModels
	if len(referenceModels) == 0 {
		referenceModels = result.Models
	}
	reference := FormulaReference{
		FormulaVersion: "LLAMBO-2", CreatedAt: createdAt.UTC(), AAVersion: result.AAVersion,
		SourceModelCounts: sourceModelCounts(referenceModels), SourceFingerprints: sourceFingerprints(referenceModels),
		Metrics: make(map[string][]float64, len(referenceMetricNames())),
	}
	reference.Metrics = currentReferenceValues(referenceModels)
	data, err := json.MarshalIndent(reference, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func currentReferenceValues(models []Model) map[string][]float64 {
	valuesByMetric := make(map[string][]float64, len(referenceSpecs()))
	for _, spec := range referenceSpecs() {
		values := make([]float64, 0, len(models))
		for _, model := range models {
			value, ok := spec.value(model)
			if ok && value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) {
				values = append(values, *value)
			}
		}
		sort.Float64s(values)
		valuesByMetric[spec.name] = values
	}
	return valuesByMetric
}

type referenceSpec struct {
	name  string
	value func(Model) (*float64, bool)
}

func referenceSpecs() []referenceSpec {
	byName := map[string]referenceSpec{}
	for _, category := range categorySpecs {
		legacy := legacyCategorySpec(category.name)
		for _, primary := range legacy.primaries {
			if containsFrozenReferenceMetric(primary.reference) {
				byName[primary.reference] = referenceSpec{primary.reference, primary.value}
			}
		}
		for _, check := range legacy.checks {
			if containsFrozenReferenceMetric(check.reference) {
				byName[check.reference] = referenceSpec{check.reference, check.value}
			}
		}
	}
	for _, metric := range operationalMetrics {
		byName[metric.name] = referenceSpec{metric.name, metric.value}
	}
	keys := make([]string, 0, len(byName))
	for key := range byName {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]referenceSpec, 0, len(keys))
	for _, key := range keys {
		result = append(result, byName[key])
	}
	return result
}

func sourceModelCounts(models []Model) map[string]int {
	counts := map[string]int{llmStatsKey: 0, artificialAnalysisKey: 0, writingbench1: 0, eqbenchCreativeV3Key: 0, ifevalOfficialKey: 0}
	for _, model := range models {
		if model.LLMStats != nil {
			counts[llmStatsKey]++
		}
		if model.AA != nil {
			counts[artificialAnalysisKey]++
		}
		if _, ok := model.Benchmarks[writingbench1]; ok {
			counts[writingbench1]++
		}
		if _, ok := model.Benchmarks[eqbenchCreativeV3]; ok {
			counts[eqbenchCreativeV3Key]++
		}
		if _, ok := model.Benchmarks[ifevalOfficial]; ok {
			counts[ifevalOfficialKey]++
		}
	}
	return counts
}

func hashJSON(value any) string {
	data, _ := json.Marshal(value)
	return sha256Hex(data)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedIntMapKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMetricMapKeys(values map[string][]float64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func twoSampleKS(left, right []float64) float64 {
	if len(left) == 0 || len(right) == 0 {
		if len(left) == len(right) {
			return 0
		}
		return 1
	}
	i, j, maximum := 0, 0, 0.0
	for i < len(left) || j < len(right) {
		var value float64
		if j >= len(right) || (i < len(left) && left[i] <= right[j]) {
			value = left[i]
		} else {
			value = right[j]
		}
		for i < len(left) && left[i] <= value {
			i++
		}
		for j < len(right) && right[j] <= value {
			j++
		}
		delta := math.Abs(float64(i)/float64(len(left)) - float64(j)/float64(len(right)))
		maximum = math.Max(maximum, delta)
	}
	return maximum
}
