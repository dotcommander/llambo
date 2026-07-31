package evals

type metricSpec struct {
	name      string
	dimension string
	source    string
	weight    float64
	higher    bool
	value     func(Model) (*float64, bool)
}

type profileSpec struct {
	name    string
	weights map[string]float64
}

var externalMetrics = []metricSpec{
	{name: "aa_intelligence_general", dimension: "general", source: "artificial_analysis", weight: .60, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Intelligence })},
	{name: "aa_coding_general", dimension: "general", source: "artificial_analysis", weight: .20, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Coding })},
	{name: "aa_agentic_general", dimension: "general", source: "artificial_analysis", weight: .20, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Agentic })},
	{name: "llm_general_index", dimension: "general", source: "llm_stats", weight: .50, higher: true, value: llmIndexValue("general")},
	{name: "llm_reasoning_general", dimension: "general", source: "llm_stats", weight: .25, higher: true, value: llmIndexValue("reasoning")},
	{name: "llm_instruction_general", dimension: "general", source: "llm_stats", weight: .15, higher: true, value: llmIndexValue("instruction_following")},
	{name: "llm_factuality_general", dimension: "general", source: "llm_stats", weight: .10, higher: true, value: llmIndexValue("factuality")},
	{name: "aa_coding_index", dimension: "coding", source: "artificial_analysis", weight: .60, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Coding })},
	{name: "aa_intelligence_coding", dimension: "coding", source: "artificial_analysis", weight: .25, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Intelligence })},
	{name: "aa_agentic_coding", dimension: "coding", source: "artificial_analysis", weight: .15, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Agentic })},
	{name: "llm_code_index", dimension: "coding", source: "llm_stats", weight: .50, higher: true, value: llmIndexValue("code", "coding")},
	{name: "llm_swe_bench_verified", dimension: "coding", source: "llm_stats", weight: .30, higher: true, value: llmValue(func(v *LLMStatsMetrics) *float64 { return v.SWEVerified })},
	{name: "llm_swe_bench_pro", dimension: "coding", source: "llm_stats", weight: .20, higher: true, value: llmValue(func(v *LLMStatsMetrics) *float64 { return v.SWEPro })},
	{name: "aa_intelligence_reasoning", dimension: "reasoning", source: "artificial_analysis", weight: .55, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Intelligence })},
	{name: "aa_coding_reasoning", dimension: "reasoning", source: "artificial_analysis", weight: .25, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Coding })},
	{name: "aa_agentic_reasoning", dimension: "reasoning", source: "artificial_analysis", weight: .20, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Agentic })},
	{name: "llm_reasoning_index", dimension: "reasoning", source: "llm_stats", weight: .60, higher: true, value: llmIndexValue("reasoning")},
	{name: "llm_general_reasoning", dimension: "reasoning", source: "llm_stats", weight: .20, higher: true, value: llmIndexValue("general")},
	{name: "llm_instruction_reasoning", dimension: "reasoning", source: "llm_stats", weight: .10, higher: true, value: llmIndexValue("instruction_following")},
	{name: "llm_structured_reasoning", dimension: "reasoning", source: "llm_stats", weight: .10, higher: true, value: llmIndexValue("structured_output")},
	{name: "aa_agentic_index", dimension: "agents", source: "artificial_analysis", weight: .60, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Agentic })},
	{name: "aa_intelligence_agents", dimension: "agents", source: "artificial_analysis", weight: .25, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Intelligence })},
	{name: "aa_coding_agents", dimension: "agents", source: "artificial_analysis", weight: .15, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.Coding })},
	{name: "llm_agents_index", dimension: "agents", source: "llm_stats", weight: .45, higher: true, value: llmIndexValue("agents")},
	{name: "llm_tool_calling_index", dimension: "agents", source: "llm_stats", weight: .35, higher: true, value: llmIndexValue("tool_calling")},
	{name: "llm_reasoning_agents", dimension: "agents", source: "llm_stats", weight: .10, higher: true, value: llmIndexValue("reasoning")},
	{name: "llm_structured_agents", dimension: "agents", source: "llm_stats", weight: .10, higher: true, value: llmIndexValue("structured_output")},
	{name: "llm_writing_index", dimension: "writing", source: "llm_stats", weight: .35, higher: true, value: llmIndexValue("writing")},
	{name: "llm_creativity_writing", dimension: "writing", source: "llm_stats", weight: .25, higher: true, value: llmIndexValue("creativity")},
	{name: "llm_language_writing", dimension: "writing", source: "llm_stats", weight: .20, higher: true, value: llmIndexValue("language")},
	{name: "llm_communication_index", dimension: "writing", source: "llm_stats", weight: .15, higher: true, value: llmIndexValue("communication")},
	{name: "llm_instruction_writing", dimension: "writing", source: "llm_stats", weight: .05, higher: true, value: llmIndexValue("instruction_following")},
	{name: "llm_long_context_index", dimension: "long-context", source: "llm_stats", weight: .50, higher: true, value: llmIndexValue("long_context")},
	{name: "llm_instruction_long", dimension: "long-context", source: "llm_stats", weight: .20, higher: true, value: llmIndexValue("instruction_following")},
	{name: "llm_factuality_long", dimension: "long-context", source: "llm_stats", weight: .15, higher: true, value: llmIndexValue("factuality")},
	{name: "llm_grounding_long", dimension: "long-context", source: "llm_stats", weight: .15, higher: true, value: llmIndexValue("grounding")},
	{name: "aa_output_speed", dimension: "speed", source: "artificial_analysis", weight: .50, higher: true, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.OutputTokensPS })},
	{name: "aa_time_to_first_token", dimension: "speed", source: "artificial_analysis", weight: .20, higher: false, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.TTFTSeconds })},
	{name: "aa_end_to_end_time", dimension: "speed", source: "artificial_analysis", weight: .30, higher: false, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.E2ESeconds })},
	{name: "llm_output_speed", dimension: "speed", source: "llm_stats", weight: 1, higher: true, value: llmValue(func(v *LLMStatsMetrics) *float64 { return v.Throughput })},
	{name: "aa_input_price", dimension: "price", source: "artificial_analysis", weight: .40, higher: false, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.InputPrice })},
	{name: "aa_output_price", dimension: "price", source: "artificial_analysis", weight: .60, higher: false, value: aaValue(func(v *ArtificialMetrics) *float64 { return v.OutputPrice })},
	{name: "llm_input_price", dimension: "price", source: "llm_stats", weight: .40, higher: false, value: llmValue(func(v *LLMStatsMetrics) *float64 { return v.InputPrice })},
	{name: "llm_output_price", dimension: "price", source: "llm_stats", weight: .60, higher: false, value: llmValue(func(v *LLMStatsMetrics) *float64 { return v.OutputPrice })},
}

var externalProfiles = []profileSpec{
	{name: "overall", weights: map[string]float64{"general": .20, "coding": .25, "reasoning": .20, "agents": .20, "writing": .10, "long-context": .05}},
	{name: "value", weights: map[string]float64{"overall": .75, "price": .25}},
}

var dimensionNames = []string{"general", "coding", "reasoning", "agents", "writing", "long-context", "speed", "price"}

func SupportedRankingProfiles() []string {
	return []string{"overall", "general", "coding", "reasoning", "agents", "writing", "long-context", "speed", "value", "price"}
}

func isSupportedProfile(name string) bool {
	for _, candidate := range SupportedRankingProfiles() {
		if name == candidate {
			return true
		}
	}
	return false
}

func profileWeights(name string) map[string]float64 {
	for _, profile := range externalProfiles {
		if profile.name == name {
			return profile.weights
		}
	}
	return map[string]float64{name: 1}
}

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

func cloneWeights(values map[string]float64) map[string]float64 {
	cloned := make(map[string]float64, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
