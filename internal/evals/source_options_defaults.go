package evals

func (o *Options) applySourceDefaults() {
	if o.LLMModelsURL == "" {
		o.LLMModelsURL = "https://api.zeroeval.com/leaderboard/models"
	}
	if o.LLMFullURL == "" {
		o.LLMFullURL = "https://api.zeroeval.com/leaderboard/models/full"
	}
	if o.LLMIndexURL == "" {
		o.LLMIndexURL = "https://api.zeroeval.com/leaderboard/indexes/compact"
	}
	if o.LLMBenchmarksURL == "" {
		o.LLMBenchmarksURL = "https://api.zeroeval.com/leaderboard/benchmarks"
	}
	if o.LLMStatsStatsV1ModelsURL == "" {
		o.LLMStatsStatsV1ModelsURL = LLMStatsStatsV1ModelsURL
	}
	if o.LLMStatsStatsV1BenchmarksURL == "" {
		o.LLMStatsStatsV1BenchmarksURL = LLMStatsStatsV1BenchmarksURL
	}
	if o.LLMStatsStatsV1ScoresURL == "" {
		o.LLMStatsStatsV1ScoresURL = LLMStatsStatsV1ScoresURL
	}
	if o.AAURL == "" {
		o.AAURL = "https://artificialanalysis.ai/api/v2/language/models/free"
	}
	if o.WritingBenchURL == "" {
		o.WritingBenchURL = "https://huggingface.co/spaces/WritingBench/WritingBench/resolve/main/score.xlsx"
	}
	if o.EQBenchCreativeURL == "" {
		o.EQBenchCreativeURL = "https://raw.githubusercontent.com/EQ-bench/EQ-bench-site/bf21868fd5dc4c48480e01ae354079eb1cec13fb/creative_writing.js"
	}
	if o.LechMazurWritingURL == "" {
		o.LechMazurWritingURL = WritingPrimaryURL
	}
	if o.ArenaCreativeURL == "" {
		o.ArenaCreativeURL = "https://raw.githubusercontent.com/lmarena/arena-catalog/main/data/leaderboard-text.json"
	}
}

func (o *Options) applyOfficialCardDefaults() {
	if o.OfficialLFMURL == "" {
		o.OfficialLFMURL = "https://huggingface.co/LiquidAI/LFM2.5-2.6B/resolve/a334ee78cd38458bb71eda24109ac42dcec1309d/README.md"
	}
	if o.OfficialQwenURL == "" {
		o.OfficialQwenURL = "https://huggingface.co/Qwen/Qwen3.8-27B/resolve/1d4bf0f2ff6012fd82039f2fa52739d0dd7c60c0/README.md"
	}
	if o.OfficialGPTOSSURL == "" {
		o.OfficialGPTOSSURL = "https://arxiv.org/html/2508.10925v1"
	}
	if o.OfficialLFMVLURL == "" {
		o.OfficialLFMVLURL = "https://huggingface.co/LiquidAI/LFM2.5-VL-3B/resolve/5a414ead75d45db003906d06fb62bd5b6846cec0/README.md"
	}
	if o.OfficialGemmaURL == "" {
		o.OfficialGemmaURL = "https://huggingface.co/google/gemma-4-31B/resolve/5bbc2fb1c1b2c611d06e3d9f23c170ba21659d89/README.md"
	}
	if o.OfficialLFM8URL == "" {
		o.OfficialLFM8URL = lfm25A1BURL
	}
	if o.officialLFM8Fetcher == nil {
		o.officialLFM8Fetcher = officialCardFetcherForSpec(officialCardDescriptors[officialLFM25A1BCard].spec)
	}
}
