package evals

import (
	"sort"
	"strings"
)

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
	{name: "agents", families: []benchmarkFamily{{"tool-api-orchestration", []string{"bfcl-v4", "toolsandbox", "tau2-bench", "tau3-bench", "tau-bench-retail", "toolbench"}}, {"environment-task-completion", []string{"gaia", "webarena", "osworld"}}, {"computer-use", []string{"assistantbench", "webarena-lite"}}}},
	{name: "coding", families: []benchmarkFamily{{"repository-editing", []string{"swe-bench-verified", "swe-bench-pro", "aider"}}, {"live-synthesis", []string{"livecodebench", "livecodebench-v6"}}, {"function-program-generation", []string{"humaneval", "mbpp", "bigcodebench"}}, {"scientific-coding", []string{"scicode", "multipl-e"}}}},
	{name: "instruction-following", families: []benchmarkFamily{{"verifiable-constraints", []string{"ifeval-official", "ifeval", "ifbench"}}, {"structured-adherence", []string{"multi-if", "ifstruct"}}, {"preference-adherence", []string{"arena-hard", "alpacaeval"}}}},
	{name: "long-context", families: []benchmarkFamily{{"retrieval-stress", []string{"ruler", "niah"}}, {"multi-document-reasoning", []string{"longbench", "longbench-v2"}}, {"persistent-state", []string{"babilong"}}}},
	{name: "reasoning", families: []benchmarkFamily{{"advanced-science", []string{"gpqa"}}, {"competition-mathematics", []string{"aime", "aime-versioned", "math"}}, {"abstraction", []string{"arc"}}, {"broad-knowledge", []string{"mmlu-pro"}}}},
	{name: "writing", families: []benchmarkFamily{{"rubric-long-form", []string{"writingbench"}}, {"fiction-narrative", []string{"eqbench-creative-v3", "fiction-live"}}, {"broad-generation", []string{"biggen"}}, {"human-preference", []string{"lmsys-writing"}}, {"style-calibration", []string{"aidanbench"}}}},
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
	{name: "aa_output_speed", dimension: "speed", source: "artificial_analysis", higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.OutputTokensPS })},
	{name: "llm_output_speed", dimension: "speed", source: "llm_stats", higher: true, value: llmValue(func(v *LLMStatsMetrics) *float64 { return v.Throughput })},
	{name: "aa_output_price", dimension: "price", source: "artificial_analysis", higher: false, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.OutputPrice })},
	{name: "llm_output_price", dimension: "price", source: "llm_stats", higher: false, value: llmValue(func(v *LLMStatsMetrics) *float64 { return v.OutputPrice })},
}

var externalMetrics = operationalMetrics

func SupportedRankingProfiles() []string {
	return []string{"agents", "coding", "instruction-following", "long-context", "matrix", "price", "reasoning", "speed", "writing"}
}
func isSupportedProfile(name string) bool {
	for _, candidate := range SupportedRankingProfiles() {
		if name == candidate {
			return true
		}
	}
	return false
}
func isCapabilityCategory(name string) bool {
	for _, category := range categorySpecs {
		if name == category.name {
			return true
		}
	}
	return false
}
func IsCapabilityCategory(name string) bool { return isCapabilityCategory(name) }

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
