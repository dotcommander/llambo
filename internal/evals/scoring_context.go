package evals

import (
	"encoding/json"
	"fmt"
)

func loadScoringContext() (scoringContext, error) {
	return loadScoringContextWithStatsV1(llmStatsStatsV1FrozenCohortsJSON)
}

func loadScoringContextWithStatsV1(data []byte) (scoringContext, error) {
	reference, err := loadFormulaReference()
	if err != nil {
		return scoringContext{}, fmt.Errorf("load formula reference: %w", err)
	}
	legacy, err := loadFrozenLLMStatsCohorts()
	if err != nil {
		return scoringContext{}, fmt.Errorf("load frozen LLM Stats cohorts: %w", err)
	}
	statsV1, err := loadFrozenLLMStatsStatsV1CohortsData(data)
	if err != nil {
		return scoringContext{}, fmt.Errorf("load frozen Stats v1 cohorts: %w", err)
	}
	return scoringContext{reference: reference, legacyCohorts: legacy, statsV1Cohorts: statsV1}, nil
}

var frozenLLMStatsCohortPopulations = map[string]int{
	longbenchV2:  17,
	"math":       71,
	"humaneval":  66,
	ifeval1:      67,
	"arena-hard": 26,
	"osworld":    20,
}

// frozenBenchmarkReference maps reviewed benchmark/revision pairs to the
// checked-in population that owns their percentile scale. It is intentionally
// not derived from a caller's cache rows: new cache evidence may add a model
// contribution, but cannot move any already-published percentile.
var frozenBenchmarkReference = map[string]map[string]string{
	gpqa1:             {publicLeaderboard: llmGpqa},
	sweBenchVerified:  {publicLeaderboard: llmSweBenchVerified},
	sweBenchPro:       {publicLeaderboard: llmSweBenchPro},
	scicode1:          {publicLeaderboard: llmScicode},
	ifevalOfficial:    {"b9aebfcbe28b6cb374042f495d733037550ab146": ifevalOfficialKey},
	writingbench1:     {"d9338ce9b09792ea7167279fee7ccc1910e28c5d": writingbenchOverall},
	eqbenchCreativeV3: {"bf21868fd5dc4c48480e01ae354079eb1cec13fb": eqbenchCreativeV3Key},
}

// officialCardFrozenCohorts are the immutable, source-native comparison
// populations from the six reviewed cards. They are intentionally values,
// not cache rows: the source adapters may add a projected artifact, but a
// later cache refresh can never move an already-issued percentile. The
// gpt-oss population is six explicitly named model+reasoning-effort arms,
// not six distinct base models.
var officialCardFrozenCohorts = map[string]map[string][]float64{
	aime1: {
		lfm25Revision:    {26.33, 34.27, 49.33, 51.87, 56.07},
		gptOSSRevision:   {50.4, 80.0, 92.5, 37.1, 72.1, 91.7},
		gemma4Revision:   {89.2, 88.3, 77.5, 42.5, 37.5, 20.8},
		lfm25A1BRevision: {42.53, 4.93, 54.28, 71.67, 26, 34.33},
	},
	livecodebenchV6: {
		lfm25Revision:  {54.92, 59.41, 60.85, 63.77, 69.86},
		gemma4Revision: {80.0, 77.1, 72.0, 52.0, 44.0, 29.1},
	},
	ifbench1: {
		lfm25Revision:    {34.08, 39.24, 48.40, 56.47, 59.17},
		qwen38Revision:   {62.5, 69.1, 77.0, 79.1, 79.5},
		lfm25A1BRevision: {56.47, 21.28, 50.38, 51.11, 33.53, 39.48, 47.25, 58.65},
	},
	multiIf: {
		lfm25Revision:    {55.67, 62.55, 69.44, 77.35, 80.07},
		lfm25A1BRevision: {79.93, 59.00, 67.43, 79.04, 69.70, 77.58, 82.06, 76.64},
	},
	ifevalOfficial: {
		lfm25A1BRevision: {91.84, 82.23, 87.80, 90.82, 82.93, 87.74, 91.40, 86.73},
	},
	ifstruct1: {
		lfm25Revision: {36.25, 64.85, 76.65, 78.50, 85.49},
	},
	bfclV4: {
		lfm25Revision:   {36.98, 46.39, 50.56, 56.88, 60.13},
		lfm25VLRevision: {32.5, 20.5, 33.2, 40.0, 33.9, 53.6},
	},
	toolsandbox1: {
		lfm25Revision:   {52.40, 65.00, 75.55, 76.44, 77.83},
		lfm25VLRevision: {59.5, 26.4, 56.5, 61.6, 47.7, 65.0},
	},
	sweBenchPro: {
		qwen38Revision: {51.2, 53.4, 53.5, 57.6, 61.7},
	},
	gpqa1: {
		qwen38Revision: {83.5, 87.8, 89.2, 90.3, 91.3},
		gptOSSRevision: {56.8, 66.0, 67.1, 73.1, 80.1, 71.5},
		gemma4Revision: {84.3, 82.3, 78.8, 58.6, 43.4, 42.4},
	},
	sweBenchVerified: {
		gptOSSRevision: {47.9, 52.6, 62.4, 37.4, 53.2, 60.7},
	},
	tauBenchRetail: {
		gptOSSRevision: {49.4, 62.0, 67.8, 35.0, 47.3, 54.8},
	},
	aider1: {
		gptOSSRevision: {24.0, 34.2, 44.4, 16.6, 26.6, 34.2},
	},
	mmluPro: {
		gemma4Revision: {85.2, 82.6, 77.2, 69.4, 60.0, 67.6},
	},
	tau2Bench: {
		gemma4Revision: {76.9, 68.2, 69.0, 42.2, 24.5, 16.2},
	},
}

// normalizedFrozenRevision maps the reviewed source's real revision format to
// the checked-in cohort key. WritingBench records its verified ETag in Version;
// a new ETag requires a reviewed reference update rather than silently sharing
// this population's percentile scale.

func loadFrozenLLMStatsStatsV1Cohorts() (frozenLLMStatsCohorts, error) {
	return loadFrozenLLMStatsStatsV1CohortsData(llmStatsStatsV1FrozenCohortsJSON)
}

func loadFrozenLLMStatsStatsV1CohortsData(data []byte) (frozenLLMStatsCohorts, error) {
	if got := sha256Hex(data); got != llmStatsStatsV1FrozenCohortsDigest {
		return frozenLLMStatsCohorts{}, fmt.Errorf("frozen Stats v1 cohort digest %s does not match pinned %s", got, llmStatsStatsV1FrozenCohortsDigest)
	}
	var cohorts frozenLLMStatsCohorts
	if err := json.Unmarshal(data, &cohorts); err != nil {
		return frozenLLMStatsCohorts{}, fmt.Errorf("decode frozen Stats v1 cohorts: %w", err)
	}
	if err := validateFrozenLLMStatsStatsV1Cohorts(cohorts); err != nil {
		return frozenLLMStatsCohorts{}, err
	}
	return cohorts, nil
}
