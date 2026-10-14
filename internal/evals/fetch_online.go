package evals

import "context"

func fetchOnlineSources(ctx context.Context, opts Options, ifeval sourceSnapshot, fetchers officialCardFetchers) (Result, error) {
	cards, cardStatuses := loadOfficialCards(ctx, opts, fetchers)
	lfm, qwen, gptOSS, lfmVL, gemma, lfm8 := cards[officialLFM25Card], cards[officialQwen38Card], cards[officialGPTOSSCard], cards[officialLFMVLCard], cards[officialGemma4Card], cards[officialLFM25A1BCard]
	lfmStatus, qwenStatus, gptOSSStatus, lfmVLStatus, gemmaStatus, lfm8Status := cardStatuses[officialLFM25Card], cardStatuses[officialQwen38Card], cardStatuses[officialGPTOSSCard], cardStatuses[officialLFMVLCard], cardStatuses[officialGemma4Card], cardStatuses[officialLFM25A1BCard]
	llm, llmStatus, err := loadSource(ctx, opts, llmStats, func(ctx context.Context) (sourceSnapshot, error) {
		return fetchLLMStats(ctx, opts)
	})
	if err != nil {
		return Result{}, err
	}
	frozenErr := prepareFrozenLLMStatsCohortResults(llm.Models, opts.CacheDir)
	aa, aaStatus, aaErr := loadArtificialAnalysis(ctx, opts)
	if aaErr != nil && !opts.AllowPartial {
		return Result{}, aaErr
	}

	writing, writingStatus, writingErr := loadSource(ctx, opts, writingbench1, func(ctx context.Context) (sourceSnapshot, error) { return fetchWritingBench(ctx, opts) })
	if writingErr != nil && !opts.AllowPartial {
		return Result{}, writingErr
	}
	eqBench, eqBenchStatus, eqBenchErr := loadSource(ctx, opts, eqbenchCreativeV3, func(ctx context.Context) (sourceSnapshot, error) { return fetchEQBenchCreative(ctx, opts) })
	if eqBenchErr != nil && !opts.AllowPartial {
		return Result{}, eqBenchErr
	}
	fiction, fictionStatus, fictionErr := loadSource(ctx, opts, WritingPrimaryID, func(ctx context.Context) (sourceSnapshot, error) { return fetchLechMazurWriting(ctx, opts) })
	if fictionErr != nil && !opts.AllowPartial {
		return Result{}, fictionErr
	}
	arena, arenaStatus, arenaErr := loadSource(ctx, opts, ArenaCreativeSourceID, func(ctx context.Context) (sourceSnapshot, error) { return fetchArenaCreative(ctx, opts) })
	if arenaErr != nil && !opts.AllowPartial {
		return Result{}, arenaErr
	}
	markSnapshotStale(&llm, llmStatus.Cache == stale1)
	markSnapshotStale(&writing, writingStatus.Cache == stale1)
	markSnapshotStale(&eqBench, eqBenchStatus.Cache == stale1)
	markSnapshotStale(&fiction, fictionStatus.Cache == stale1)
	markSnapshotStale(&arena, arenaStatus.Cache == stale1)
	sourceModels := evalsSourceModels(evalsSourceSet{
		llm: llm.Models, aa: aa.Models, writing: writing.Models, eqBench: eqBench.Models,
		fiction: fiction.Models, arena: arena.Models, ifeval: ifeval.Models,
		lfm: lfm.Models, qwen: qwen.Models, gptOSS: gptOSS.Models, lfmVL: lfmVL.Models,
		gemma: gemma.Models, lfm8: lfm8.Models,
	})
	models := mergeWritingEvidenceModels(mergeModels(llm.Models, aa.Models), writing.Models, eqBench.Models, fiction.Models, arena.Models, ifeval.Models)
	models = mergeOfficialCardModels(models, lfm.Models, qwen.Models, gptOSS.Models, lfmVL.Models, gemma.Models, lfm8.Models)
	return Result{
		GeneratedAt:     opts.Now().UTC(),
		Models:          models,
		SourceModels:    sourceModels,
		ReferenceModels: sourceNativeModels(llm.Models, aa.Models, writing.Models, eqBench.Models, fiction.Models, arena.Models, ifeval.Models, lfm.Models, qwen.Models, gptOSS.Models, lfmVL.Models, gemma.Models, lfm8.Models),
		Sources:         []SourceStatus{llmStatus, frozenLLMStatsCohortStatus(frozenErr), aaStatus, writingStatus, eqBenchStatus, fictionStatus, arenaStatus, statusFor("Official IFEval", ifeval.URL, ifeval, "frozen", nil), lfmStatus, qwenStatus, gptOSSStatus, lfmVLStatus, gemmaStatus, lfm8Status},
		AAVersion:       aa.AAVersion,
	}, nil
}
