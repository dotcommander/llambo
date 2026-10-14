package evals

func sourceCacheMetadata(opts Options, cacheName string) (name, url string) {
	name, url = "LLM Stats", LLMStatsLeaderboardURL
	if cacheName == artificialAnalysis {
		name, url = artificialAnalysisName, ArtificialAnalysisURL
	}
	if cacheName == writingbench1 {
		name, url = writingBench1, opts.WritingBenchURL
	}
	if cacheName == eqbenchCreativeV3 {
		name, url = eQBenchCreativeV3, opts.EQBenchCreativeURL
	}
	if cacheName == WritingPrimaryID {
		name, url = lechMazurCreativeStoryWriting, opts.LechMazurWritingURL
	}
	if cacheName == ArenaCreativeSourceID {
		name, url = arenaCreativeWriting, opts.ArenaCreativeURL
	}
	if cacheName == officialLfm25X2X6b {
		name, url = liquidAILFM2X5X2X6BCard, opts.OfficialLFMURL
	}
	if cacheName == officialQwen3X8X27b {
		name, url = qwen3X8X27BCard, opts.OfficialQwenURL
	}
	if cacheName == officialGptOssX20b {
		name, url = openAIGptOssModelCard, opts.OfficialGPTOSSURL
	}
	if cacheName == officialLfm25VlX3b {
		name, url = liquidAILFM2X5VLX3BCard, opts.OfficialLFMVLURL
	}
	if cacheName == officialGemma4 {
		name, url = googleGemmaX4ModelCard, opts.OfficialGemmaURL
	}
	if cacheName == officialLFM25A1BCacheName {
		name, url = "LiquidAI LFM2.5-8B-A1B card", opts.OfficialLFM8URL
	}
	return name, url
}
