package evals

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

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

// formulaReferenceDigest pins the exact, versioned LLAMBO-2 cohort. Source
// refreshes must generate and review a new formula version rather than silently
// changing the percentile population used by an installed binary.
const formulaReferenceDigest = "74d5049896f5c8de4c01d22d15357e5451209d215b7e6410b9169dc6cb30ffed"

const llmStatsFrozenCohortsArtifactDigest = "de5462f48f2ac7d36713cdf96b57d63832588e598d0bae54e02f7c47737a4f8a"
const llmStatsFrozenCohortsDigest = "0e47a655d96911e5f8db8ed83b645adf352d66b4dda803813e91ab1ff92ff8bb"

var frozenReferenceMetrics = []string{
	"aa_agentic_coding", "aa_agentic_general", "aa_agentic_index", "aa_agentic_reasoning",
	"aa_coding_agents", "aa_coding_general", "aa_coding_index", "aa_coding_reasoning",
	"aa_end_to_end_time", "aa_input_price", "aa_intelligence_agents", "aa_intelligence_coding",
	"aa_intelligence_general", "aa_intelligence_reasoning", "aa_output_price", "aa_output_speed",
	"aa_time_to_first_token", "eqbench_creative_v3", "ifeval_official", "llm_agents_index",
	"llm_code_index", "llm_communication_index", "llm_creativity_writing", "llm_factuality_general",
	"llm_factuality_long", "llm_general_index", "llm_general_reasoning", "llm_gpqa", "llm_grounding_long",
	"llm_input_price", "llm_instruction_general", "llm_instruction_long", "llm_instruction_reasoning",
	"llm_instruction_writing", "llm_language_writing", "llm_long_context_index", "llm_mcp_atlas",
	"llm_output_price", "llm_output_speed", "llm_reasoning_agents", "llm_reasoning_general",
	"llm_reasoning_index", "llm_scicode", "llm_structured_agents", "llm_structured_reasoning",
	"llm_swe_bench_pro", "llm_swe_bench_verified", "llm_tool_calling_index", "llm_writing_index",
	"writingbench_domain", "writingbench_format", "writingbench_length", "writingbench_overall", "writingbench_requirements",
}

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

var frozenLLMStatsCohortPopulations = map[string]int{
	"longbench-v2": 17,
	"math":         71,
	"humaneval":    66,
	"ifeval":       67,
	"arena-hard":   26,
	"osworld":      20,
}

// frozenBenchmarkReference maps reviewed benchmark/revision pairs to the
// checked-in population that owns their percentile scale. It is intentionally
// not derived from a caller's cache rows: new cache evidence may add a model
// contribution, but cannot move any already-published percentile.
var frozenBenchmarkReference = map[string]map[string]string{
	"gpqa":                {"public-leaderboard": "llm_gpqa"},
	"swe-bench-verified":  {"public-leaderboard": "llm_swe_bench_verified"},
	"swe-bench-pro":       {"public-leaderboard": "llm_swe_bench_pro"},
	"scicode":             {"public-leaderboard": "llm_scicode"},
	"ifeval-official":     {"b9aebfcbe28b6cb374042f495d733037550ab146": "ifeval_official"},
	"writingbench":        {"d9338ce9b09792ea7167279fee7ccc1910e28c5d": "writingbench_overall"},
	"eqbench-creative-v3": {"bf21868fd5dc4c48480e01ae354079eb1cec13fb": "eqbench_creative_v3"},
}

// officialCardFrozenCohorts are the immutable, source-native comparison
// populations from the five reviewed cards. They are intentionally values,
// not cache rows: the source adapters may add a projected artifact, but a
// later cache refresh can never move an already-issued percentile. The
// gpt-oss population is six explicitly named model+reasoning-effort arms,
// not six distinct base models.
var officialCardFrozenCohorts = map[string]map[string][]float64{
	"aime": {
		lfm25Revision:  {26.33, 34.27, 49.33, 51.87, 56.07},
		gptOSSRevision: {50.4, 80.0, 92.5, 37.1, 72.1, 91.7},
		gemma4Revision: {89.2, 88.3, 77.5, 42.5, 37.5, 20.8},
	},
	"livecodebench-v6": {
		lfm25Revision:  {54.92, 59.41, 60.85, 63.77, 69.86},
		gemma4Revision: {80.0, 77.1, 72.0, 52.0, 44.0, 29.1},
	},
	"ifbench": {
		lfm25Revision:  {34.08, 39.24, 48.40, 56.47, 59.17},
		qwen38Revision: {62.5, 69.1, 77.0, 79.1, 79.5},
	},
	"multi-if": {
		lfm25Revision: {55.67, 62.55, 69.44, 77.35, 80.07},
	},
	"ifstruct": {
		lfm25Revision: {36.25, 64.85, 76.65, 78.50, 85.49},
	},
	"bfcl-v4": {
		lfm25Revision:   {36.98, 46.39, 50.56, 56.88, 60.13},
		lfm25VLRevision: {32.5, 20.5, 33.2, 40.0, 33.9, 53.6},
	},
	"toolsandbox": {
		lfm25Revision:   {52.40, 65.00, 75.55, 76.44, 77.83},
		lfm25VLRevision: {59.5, 26.4, 56.5, 61.6, 47.7, 65.0},
	},
	"swe-bench-pro": {
		qwen38Revision: {51.2, 53.4, 53.5, 57.6, 61.7},
	},
	"gpqa": {
		qwen38Revision: {83.5, 87.8, 89.2, 90.3, 91.3},
		gptOSSRevision: {56.8, 66.0, 67.1, 73.1, 80.1, 71.5},
		gemma4Revision: {84.3, 82.3, 78.8, 58.6, 43.4, 42.4},
	},
	"swe-bench-verified": {
		gptOSSRevision: {47.9, 52.6, 62.4, 37.4, 53.2, 60.7},
	},
	"tau-bench-retail": {
		gptOSSRevision: {49.4, 62.0, 67.8, 35.0, 47.3, 54.8},
	},
	"aider": {
		gptOSSRevision: {24.0, 34.2, 44.4, 16.6, 26.6, 34.2},
	},
	"mmlu-pro": {
		gemma4Revision: {85.2, 82.6, 77.2, 69.4, 60.0, 67.6},
	},
	"tau2-bench": {
		gemma4Revision: {76.9, 68.2, 69.0, 42.2, 24.5, 16.2},
	},
}

// normalizedFrozenRevision maps the reviewed source's real revision format to
// the checked-in cohort key. WritingBench records its verified ETag in Version;
// a new ETag requires a reviewed reference update rather than silently sharing
// this population's percentile scale.
func normalizedFrozenRevision(benchmark string, result BenchmarkResult) string {
	version := strings.TrimSpace(result.Version)
	switch benchmark {
	case "writingbench":
		return version
	case "ifeval-official", "eqbench-creative-v3":
		return strings.TrimSpace(result.CommitSHA)
	}
	return version
}

func frozenBenchmarkCohort(benchmark, revision string) []float64 {
	if cohort := officialCardFrozenCohorts[benchmark][revision]; len(cohort) != 0 {
		clone := append([]float64(nil), cohort...)
		sort.Float64s(clone)
		return clone
	}
	metric, ok := frozenBenchmarkReference[benchmark][revision]
	if !ok {
		return nil
	}
	reference, err := loadFormulaReference()
	if err != nil {
		return nil
	}
	values := reference.Metrics[metric]
	if len(values) == 0 {
		return nil
	}
	return append([]float64(nil), values...)
}

// resolvedFrozenBenchmarkCohort is the scorer boundary for immutable
// benchmark populations. The six LLM Stats cohorts need the complete result
// because one benchmark-wide population spans several sealed page digests.
// Every such row must therefore prove its exact membership contract before it
// can use the frozen scale. Older reviewed cohorts retain their existing
// revision-only lookup contracts.
func resolvedFrozenBenchmarkCohort(benchmark string, result BenchmarkResult) []float64 {
	if _, targeted := frozenLLMStatsCohortPopulations[benchmark]; !targeted {
		return frozenBenchmarkCohort(benchmark, normalizedFrozenRevision(benchmark, result))
	}
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		return nil
	}
	cohort, ok := cohorts.Benchmarks[benchmark]
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

func validateFrozenLLMStatsCohorts(cohorts frozenLLMStatsCohorts) error {
	if cohorts.SchemaVersion != "llm-stats-frozen-cohorts-v1" || cohorts.SealedArtifactSHA256 != llmStatsFrozenCohortsArtifactDigest {
		return fmt.Errorf("frozen LLM Stats cohorts do not match the sealed artifact")
	}
	if len(cohorts.Benchmarks) != len(frozenLLMStatsCohortPopulations) {
		return fmt.Errorf("frozen LLM Stats cohorts have %d benchmarks, want %d", len(cohorts.Benchmarks), len(frozenLLMStatsCohortPopulations))
	}
	for benchmark, population := range frozenLLMStatsCohortPopulations {
		cohort, ok := cohorts.Benchmarks[benchmark]
		if !ok || cohort.SourceID != "llm-stats-benchmark-results" || cohort.SourceClass != string(SourceAggregatorResult) || cohort.BenchmarkVersion != "public-leaderboard" || cohort.Cohort != "public" || cohort.Methodology != "LLM Stats public leaderboard aggregation; verification status supplied per row" || cohort.Direction != "higher" || cohort.MinPopulation != 5 || cohort.RowCount != population || len(cohort.Scores) != population || len(cohort.Digests) == 0 {
			return fmt.Errorf("frozen LLM Stats cohort %s has an invalid contract", benchmark)
		}
		for index, digest := range cohort.Digests {
			if !validSHA256(digest) || index > 0 && cohort.Digests[index-1] >= digest {
				return fmt.Errorf("frozen LLM Stats cohort %s has invalid digests", benchmark)
			}
		}
		for index, score := range cohort.Scores {
			if !finite(score) || index > 0 && cohort.Scores[index-1] > score {
				return fmt.Errorf("frozen LLM Stats cohort %s has invalid scores", benchmark)
			}
		}
	}
	for benchmark := range cohorts.Benchmarks {
		if _, ok := frozenLLMStatsCohortPopulations[benchmark]; !ok {
			return fmt.Errorf("frozen LLM Stats cohorts have unexpected benchmark %s", benchmark)
		}
	}
	return nil
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
	if _, targeted := frozenLLMStatsCohortPopulations[benchmark]; !targeted {
		return ""
	}
	cohorts, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		return "frozen LLM Stats cohort is unavailable"
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
	names := make([]string, 0, len(set))
	for benchmark := range set {
		names = append(names, benchmark)
	}
	sort.Strings(names)
	return names
}

type FormulaReferenceSummary struct {
	CreatedAt          time.Time         `json:"created_at"`
	AAVersion          float64           `json:"artificial_analysis_index_version"`
	SourceModelCounts  map[string]int    `json:"source_model_counts"`
	SourceFingerprints map[string]string `json:"source_fingerprints"`
}

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
		return FormulaReference{}, fmt.Errorf("formula reference is missing or does not match LLAMBO-2 rollback diagnostics")
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

// formulaReferenceSpecs is the checked-in snapshot inventory used by tests and
// reference generation. Primary sources absent from this versioned snapshot
// deliberately yield nil category scores rather than synthetic percentiles.
func formulaReferenceSpecs() []referenceSpec {
	return referenceSpecs()
}

func sourceModelCounts(models []Model) map[string]int {
	counts := map[string]int{"llm_stats": 0, "artificial_analysis": 0, "writingbench": 0, "eqbench_creative_v3": 0, "ifeval_official": 0}
	for _, model := range models {
		if model.LLMStats != nil {
			counts["llm_stats"]++
		}
		if model.AA != nil {
			counts["artificial_analysis"]++
		}
		if _, ok := model.Benchmarks["writingbench"]; ok {
			counts["writingbench"]++
		}
		if _, ok := model.Benchmarks["eqbench-creative-v3"]; ok {
			counts["eqbench_creative_v3"]++
		}
		if _, ok := model.Benchmarks["ifeval-official"]; ok {
			counts["ifeval_official"]++
		}
	}
	return counts
}

func sourceFingerprints(models []Model) map[string]string {
	type row struct {
		Key          string             `json:"key"`
		Name         string             `json:"name"`
		Organization string             `json:"organization"`
		LLM          *LLMStatsMetrics   `json:"llm,omitempty"`
		AA           *ArtificialMetrics `json:"aa,omitempty"`
	}
	llmRows, aaRows := make([]row, 0), make([]row, 0)
	for _, model := range models {
		if model.LLMStats != nil {
			llmRows = append(llmRows, row{Key: model.Key, Name: model.Name, Organization: model.Organization, LLM: model.LLMStats})
		}
		if model.AA != nil {
			key := model.Key
			name, organization := model.AA.sourceName, model.AA.sourceOrganization
			if name == "" {
				name = model.Name
			}
			if organization == "" {
				organization = model.Organization
			}
			if model.AA.Slug != "" {
				key = model.AA.Slug
			}
			aaRows = append(aaRows, row{Key: key, Name: name, Organization: organization, AA: model.AA})
		}
	}
	sort.Slice(llmRows, func(i, j int) bool { return llmRows[i].Key < llmRows[j].Key })
	sort.Slice(aaRows, func(i, j int) bool { return aaRows[i].Key < aaRows[j].Key })
	fingerprints := map[string]string{"llm_stats": hashJSON(llmRows), "artificial_analysis": hashJSON(aaRows)}
	for _, source := range []struct{ key, benchmark string }{{"writingbench", "writingbench"}, {"eqbench_creative_v3", "eqbench-creative-v3"}, {"ifeval_official", "ifeval-official"}} {
		for _, model := range models {
			if result, ok := model.Benchmarks[source.benchmark]; ok && result.ContentSHA != "" {
				fingerprints[source.key] = result.ContentSHA
				break
			}
		}
	}
	return fingerprints
}

func hashJSON(value any) string {
	data, _ := json.Marshal(value)
	return sha256Hex(data)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func evaluateReferenceDrift(models []Model, aaVersion float64, reference FormulaReference, currentValues map[string][]float64) DriftDiagnostics {
	drift := DriftDiagnostics{Status: "stable", CurrentFingerprints: sourceFingerprints(models)}
	if aaVersion != 0 && reference.AAVersion != 0 && aaVersion != reference.AAVersion {
		drift.Reasons = append(drift.Reasons, fmt.Sprintf("Artificial Analysis index changed from %.1f to %.1f", reference.AAVersion, aaVersion))
	}
	counts := sourceModelCounts(models)
	for _, source := range sortedStringMapKeys(reference.SourceFingerprints) {
		current := drift.CurrentFingerprints[source]
		if current != "" && current != reference.SourceFingerprints[source] {
			drift.Reasons = append(drift.Reasons, fmt.Sprintf("%s source fingerprint changed", source))
		}
	}
	for _, source := range sortedIntMapKeys(reference.SourceModelCounts) {
		referenceCount := reference.SourceModelCounts[source]
		if referenceCount == 0 {
			continue
		}
		change := math.Abs(float64(counts[source]-referenceCount)) / float64(referenceCount)
		if change > .20 {
			drift.Reasons = append(drift.Reasons, fmt.Sprintf("%s model count changed %.1f%%", source, change*100))
		}
	}
	metricSources := make(map[string]string, len(operationalMetrics))
	for _, spec := range operationalMetrics {
		metricSources[spec.name] = spec.source
	}
	for _, name := range []string{"writingbench_overall", "writingbench_format", "writingbench_length", "writingbench_domain", "writingbench_requirements"} {
		metricSources[name] = "writingbench"
	}
	metricSources["eqbench_creative_v3"] = "eqbench_creative_v3"
	metricSources["ifeval_official"] = "ifeval_official"
	for _, name := range sortedMetricMapKeys(reference.Metrics) {
		referenceValues := reference.Metrics[name]
		current := currentValues[name]
		ks := twoSampleKS(referenceValues, current)
		if ks > drift.MaxKS {
			drift.MaxKS, drift.WorstMetric = ks, name
		}
		source := metricSources[name]
		referencePopulation, currentPopulation := reference.SourceModelCounts[source], counts[source]
		if referencePopulation > 0 && currentPopulation > 0 {
			referenceCoverage := float64(len(referenceValues)) / float64(referencePopulation)
			currentCoverage := float64(len(current)) / float64(currentPopulation)
			coverageChange := math.Abs(currentCoverage - referenceCoverage)
			if coverageChange > .10 {
				drift.Reasons = append(drift.Reasons, fmt.Sprintf("%s coverage changed %.1f percentage points", name, coverageChange*100))
			}
		}
	}
	if drift.MaxKS > .15 {
		drift.Reasons = append(drift.Reasons, fmt.Sprintf("%s distribution KS %.3f exceeds 0.150", drift.WorstMetric, drift.MaxKS))
	}
	if len(drift.Reasons) > 0 {
		drift.Status = "drifted"
	}
	return drift
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
