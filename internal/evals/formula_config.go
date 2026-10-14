package evals

import (
	"sort"
	"strings"
)

// Repeated string literals consolidated by goconst.
const (
	agents1              = "agents"
	coding1              = "coding"
	instructionFollowing = "instruction-following"
	longContext          = "long-context"
	price1               = "price"
	reasoning1           = "reasoning"
	writing1             = "writing"
)

const speed1 = "speed"

type metricSpec struct {
	name      string
	dimension string
	source    string
	higher    bool
	value     func(Model) (*float64, bool)
}

type benchmarkSpec struct {
	benchmark string
	name      string
	reference string
	value     func(Model) (*float64, bool)
}

type categorySpec struct {
	name     string
	families []benchmarkFamily
	// Retained for LLAMBO-2 reference decoding and rollback diagnostics.
	primaries []benchmarkSpec
	checks    []benchmarkSpec
}

type benchmarkFamily struct {
	name       string
	benchmarks []string
}

// categorySpecs is the reviewed LLAMBO-6 portfolio. A family represents one
// independent capability signal, so every represented family has equal weight.
// Benchmarks within a family are alternatives, not additional votes. This keeps
// mirrored or near-duplicate leaderboards from dominating a category.
var categorySpecs = []categorySpec{
	{name: agents1, families: []benchmarkFamily{{"tool-api-orchestration", []string{bfclV4, toolsandbox1, tau2Bench, "tau3-bench", tauBenchRetail, "toolbench"}}, {"environment-task-completion", []string{"gaia", "webarena", "osworld"}}, {"computer-use", []string{"assistantbench", "webarena-lite"}}}},
	{name: coding1, families: []benchmarkFamily{{"repository-editing", []string{sweBenchVerified, sweBenchPro, aider1}}, {"live-synthesis", []string{"livecodebench", livecodebenchV6}}, {"function-program-generation", []string{"humaneval", "mbpp", "bigcodebench"}}, {"scientific-coding", []string{scicode1, "multipl-e"}}}},
	{name: instructionFollowing, families: []benchmarkFamily{{"verifiable-constraints", []string{ifevalOfficial, ifeval1, ifbench1}}, {"structured-adherence", []string{multiIf, ifstruct1}}, {"preference-adherence", []string{"arena-hard", "alpacaeval"}}}},
	{name: longContext, families: []benchmarkFamily{{"retrieval-stress", []string{ruler1, "niah"}}, {"multi-document-reasoning", []string{"longbench", longbenchV2}}, {"persistent-state", []string{"babilong"}}}},
	{name: reasoning1, families: []benchmarkFamily{{"advanced-science", []string{gpqa1}}, {"competition-mathematics", []string{aime1, "aime-versioned", "math"}}, {"abstraction", []string{"arc"}}, {"broad-knowledge", []string{mmluPro}}}},
	{name: writing1, families: []benchmarkFamily{{"rubric-long-form", []string{writingbench1}}, {"fiction-narrative", []string{eqbenchCreativeV3, "fiction-live"}}, {"broad-generation", []string{"biggen"}}, {"human-preference", []string{"lmsys-writing"}}, {"style-calibration", []string{"aidanbench"}}}},
}

func benchmarkDetailAverageValue(benchmark string, keys ...string) func(Model) (*float64, bool) {
	return func(model Model) (*float64, bool) {
		result, ok := model.Benchmarks[benchmark]
		if !ok || len(keys) == 0 {
			return nil, false
		}
		total := 0.0
		for _, key := range keys {
			value, present := result.Details[key]
			if !present {
				return nil, false
			}
			total += value
		}
		value := total / float64(len(keys))
		return &value, true
	}
}

func benchmarkDetailPrefixAverageValue(benchmark string, prefixes ...string) func(Model) (*float64, bool) {
	return func(model Model) (*float64, bool) {
		result, ok := model.Benchmarks[benchmark]
		if !ok {
			return nil, false
		}
		matching := make([]string, 0, len(result.Details))
		for key := range result.Details {
			for _, prefix := range prefixes {
				if strings.HasPrefix(key, prefix) {
					matching = append(matching, key)
					break
				}
			}
		}
		if len(matching) == 0 {
			return nil, false
		}
		sort.Strings(matching)
		total := 0.0
		for _, key := range matching {
			total += result.Details[key]
		}
		value := total / float64(len(matching))
		return &value, true
	}
}

var operationalMetrics = []metricSpec{
	{name: "aa_output_speed", dimension: speed1, source: artificialAnalysisKey, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.OutputTokensPS })},
	{name: "llm_output_speed", dimension: speed1, source: llmStatsKey, higher: true, value: llmValue(func(v *LLMStatsMetrics) *float64 { return v.Throughput })},
	{name: "aa_output_price", dimension: price1, source: artificialAnalysis, higher: false, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.OutputPrice })},
	{name: "llm_output_price", dimension: price1, source: llmStats, higher: false, value: llmValue(func(v *LLMStatsMetrics) *float64 { return v.OutputPrice })},
}

var externalMetrics = operationalMetrics

// SupportedRankingProfiles lists the valid ranking profile names.
func SupportedRankingProfiles() []string {
	return []string{agents1, coding1, instructionFollowing, longContext, matrix1, price1, reasoning1, speed1, writing1}
}
func isSupportedProfile(name string) bool {
	for _, candidate := range SupportedRankingProfiles() {
		if name == candidate {
			return true
		}
	}
	return false
}
func validCapabilityCategory(name string) bool {
	for _, category := range categorySpecs {
		if name == category.name {
			return true
		}
	}
	return false
}

// IsCapabilityCategory reports whether a name is a valid capability category.
func IsCapabilityCategory(name string) bool { return validCapabilityCategory(name) }

func aaValue(getter func(*ArtificialMetrics) *float64) func(Model) (*float64, bool) {
	return func(model Model) (*float64, bool) {
		if model.AA == nil {
			return nil, false
		}
		value := getter(model.AA)
		return value, value != nil
	}
}
func llmValue(getter func(*LLMStatsMetrics) *float64) func(Model) (*float64, bool) {
	return func(model Model) (*float64, bool) {
		if model.LLMStats == nil {
			return nil, false
		}
		value := getter(model.LLMStats)
		return value, value != nil
	}
}
func llmIndexValue(names ...string) func(Model) (*float64, bool) {
	return func(model Model) (*float64, bool) {
		if model.LLMStats == nil {
			return nil, false
		}
		for _, name := range names {
			if index, ok := model.LLMStats.Indexes[name]; ok {
				value := index.Conservative
				return &value, true
			}
		}
		return nil, false
	}
}
func benchmarkValue(name string) func(Model) (*float64, bool) {
	return func(model Model) (*float64, bool) {
		result, ok := model.Benchmarks[name]
		return result.Score, ok && result.Score != nil
	}
}
