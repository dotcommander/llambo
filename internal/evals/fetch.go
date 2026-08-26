package evals

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"
)

const maxSourceBody = 32 << 20

type officialCardFetcher func(context.Context, Options) (sourceSnapshot, error)

type sourceSnapshot struct {
	FetchedAt       time.Time `json:"fetched_at"`
	Models          []Model   `json:"models"`
	AAVersion       float64   `json:"aa_version,omitempty"`
	URL             string    `json:"url,omitempty"`
	Version         string    `json:"version,omitempty"`
	CommitSHA       string    `json:"commit_sha,omitempty"`
	ContentSHA      string    `json:"content_sha256,omitempty"`
	Method          string    `json:"methodology,omitempty"`
	Observations    int       `json:"observations,omitempty"`
	RegistryVersion string    `json:"source_registry_version,omitempty"`
	EvidenceGrade   string    `json:"evidence_grade,omitempty"`
}

func Fetch(ctx context.Context, opts Options) (Result, error) {
	opts.applyDefaults()
	if opts.Refresh && opts.RefreshOfficialCards {
		return Result{}, fmt.Errorf("global refresh and official-card refresh cannot be combined")
	}
	if opts.Offline && opts.RefreshOfficialCards {
		return Result{}, fmt.Errorf("offline and official-card refresh cannot be combined")
	}
	ifeval, err := loadOfficialIFEvalSnapshot()
	if err != nil {
		return Result{}, err
	}
	if opts.Offline {
		return fetchCachedOnly(opts, ifeval)
	}
	if opts.RefreshOfficialCards {
		statuses, err := refreshOfficialModelCards(ctx, opts)
		if err != nil {
			return Result{}, err
		}
		result, err := fetchCachedOnly(opts, ifeval)
		if err != nil {
			return Result{}, err
		}
		if err := mergeRefreshedStatuses(result.Sources, statuses); err != nil {
			return Result{}, err
		}
		return result, nil
	}
	var lfm, qwen, gptOSS, lfmVL, gemma, lfm8 sourceSnapshot
	var lfmStatus, qwenStatus, gptOSSStatus, lfmVLStatus, gemmaStatus, lfm8Status SourceStatus
	if opts.Refresh {
		lfm, lfmStatus = loadOptionalOfficialSource(ctx, opts, "official-lfm25-2.6b", func(ctx context.Context) (sourceSnapshot, error) {
			return fetchOfficialLFM25(ctx, opts)
		})
		qwen, qwenStatus = loadOptionalOfficialSource(ctx, opts, "official-qwen3.8-27b", func(ctx context.Context) (sourceSnapshot, error) {
			return fetchOfficialQwen38(ctx, opts)
		})
		gptOSS, gptOSSStatus = loadOptionalOfficialSource(ctx, opts, "official-gpt-oss-20b", func(ctx context.Context) (sourceSnapshot, error) {
			return fetchOfficialGPTOSS(ctx, opts)
		})
		lfmVL, lfmVLStatus = loadOptionalOfficialSource(ctx, opts, "official-lfm25-vl-3b", func(ctx context.Context) (sourceSnapshot, error) {
			return fetchOfficialLFMVL(ctx, opts)
		})
		gemma, gemmaStatus = loadOptionalOfficialSource(ctx, opts, "official-gemma4", func(ctx context.Context) (sourceSnapshot, error) {
			return fetchOfficialGemma4(ctx, opts)
		})
		lfm8, lfm8Status = loadOptionalOfficialSource(ctx, opts, officialLFM25A1BCacheName, func(ctx context.Context) (sourceSnapshot, error) {
			return fetchOfficialLFM25A1B(ctx, opts)
		})
	} else {
		lfm, lfmStatus = readOptionalOfficialSnapshot(opts, "official-lfm25-2.6b")
		qwen, qwenStatus = readOptionalOfficialSnapshot(opts, "official-qwen3.8-27b")
		gptOSS, gptOSSStatus = readOptionalOfficialSnapshot(opts, "official-gpt-oss-20b")
		lfmVL, lfmVLStatus = readOptionalOfficialSnapshot(opts, "official-lfm25-vl-3b")
		gemma, gemmaStatus = readOptionalOfficialSnapshot(opts, "official-gemma4")
		lfm8, lfm8Status = readOptionalOfficialSnapshot(opts, officialLFM25A1BCacheName)
	}
	llm, llmStatus, err := loadSource(ctx, opts, "llm-stats", func(ctx context.Context) (sourceSnapshot, error) {
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

	writing, writingStatus, writingErr := loadSource(ctx, opts, "writingbench", func(ctx context.Context) (sourceSnapshot, error) { return fetchWritingBench(ctx, opts) })
	if writingErr != nil && !opts.AllowPartial {
		return Result{}, writingErr
	}
	eqBench, eqBenchStatus, eqBenchErr := loadSource(ctx, opts, "eqbench-creative-v3", func(ctx context.Context) (sourceSnapshot, error) { return fetchEQBenchCreative(ctx, opts) })
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
	markSnapshotStale(&llm, llmStatus.Cache == "stale")
	markSnapshotStale(&writing, writingStatus.Cache == "stale")
	markSnapshotStale(&eqBench, eqBenchStatus.Cache == "stale")
	markSnapshotStale(&fiction, fictionStatus.Cache == "stale")
	markSnapshotStale(&arena, arenaStatus.Cache == "stale")
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

var (
	fetchOfficialLFM25Source                   = fetchOfficialLFM25
	fetchOfficialQwen38Source                  = fetchOfficialQwen38
	fetchOfficialGPTOSSSource                  = fetchOfficialGPTOSS
	fetchOfficialLFMVLSource                   = fetchOfficialLFMVL
	fetchOfficialGemmaSource                   = fetchOfficialGemma4
	loadFrozenLLMStatsCachedObservationsSource = loadFrozenLLMStatsCachedObservations
)

// refreshOfficialModelCards is intentionally a source-scoped network path.
// It forces only the six reviewed card adapters to refresh, then leaves the
// remaining result assembly to the cache-only path.
func refreshOfficialModelCards(ctx context.Context, opts Options) ([]SourceStatus, error) {
	refresh := opts
	refresh.Refresh = true
	tasks := []struct {
		cache string
		fetch func(context.Context, Options) (sourceSnapshot, error)
	}{
		{"official-lfm25-2.6b", fetchOfficialLFM25Source},
		{"official-qwen3.8-27b", fetchOfficialQwen38Source},
		{"official-gpt-oss-20b", fetchOfficialGPTOSSSource},
		{"official-lfm25-vl-3b", fetchOfficialLFMVLSource},
		{"official-gemma4", fetchOfficialGemmaSource},
		{officialLFM25A1BCacheName, opts.officialLFM8Fetcher},
	}
	statuses := make([]SourceStatus, 0, len(tasks))
	for _, task := range tasks {
		snapshot, status, err := loadSource(ctx, refresh, task.cache, func(ctx context.Context) (sourceSnapshot, error) {
			return task.fetch(ctx, refresh)
		})
		if err != nil {
			return nil, fmt.Errorf("refresh official model card %q: %w", task.cache, err)
		}
		if status.Cache != "fetched" {
			return nil, fmt.Errorf("refresh official model card %q did not fetch", task.cache)
		}
		if err := validateOfficialCardSnapshot(task.cache, snapshot); err != nil {
			return nil, fmt.Errorf("refresh official model card %q: %w", task.cache, err)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func fetchCachedOnly(opts Options, ifeval sourceSnapshot) (Result, error) {
	lfm, lfmStatus := readOptionalOfficialSnapshot(opts, "official-lfm25-2.6b")
	qwen, qwenStatus := readOptionalOfficialSnapshot(opts, "official-qwen3.8-27b")
	gptOSS, gptOSSStatus := readOptionalOfficialSnapshot(opts, "official-gpt-oss-20b")
	lfmVL, lfmVLStatus := readOptionalOfficialSnapshot(opts, "official-lfm25-vl-3b")
	gemma, gemmaStatus := readOptionalOfficialSnapshot(opts, "official-gemma4")
	lfm8, lfm8Status := readOptionalOfficialSnapshot(opts, officialLFM25A1BCacheName)
	llm, err := readSnapshot(filepath.Join(opts.CacheDir, "llm-stats.json"))
	if err != nil {
		return Result{}, fmt.Errorf("read cached LLM Stats: %w", err)
	}
	frozenErr := prepareFrozenLLMStatsCohortResults(llm.Models, opts.CacheDir)
	aa, err := readSnapshot(filepath.Join(opts.CacheDir, "artificial-analysis.json"))
	if err != nil && !opts.AllowPartial {
		return Result{}, fmt.Errorf("read cached Artificial Analysis: %w", err)
	}
	writing, writingErr := readSnapshot(filepath.Join(opts.CacheDir, "writingbench.json"))
	eqBench, eqBenchErr := readSnapshot(filepath.Join(opts.CacheDir, "eqbench-creative-v3.json"))
	fiction, fictionErr := readSnapshot(filepath.Join(opts.CacheDir, WritingPrimaryID+".json"))
	arena, arenaErr := readSnapshot(filepath.Join(opts.CacheDir, ArenaCreativeSourceID+".json"))
	markSnapshotStale(&llm, opts.Now().Sub(llm.FetchedAt) > opts.TTL)
	markSnapshotStale(&writing, !writing.FetchedAt.IsZero() && opts.Now().Sub(writing.FetchedAt) > opts.TTL)
	markSnapshotStale(&eqBench, !eqBench.FetchedAt.IsZero() && opts.Now().Sub(eqBench.FetchedAt) > opts.TTL)
	markSnapshotStale(&fiction, !fiction.FetchedAt.IsZero() && opts.Now().Sub(fiction.FetchedAt) > opts.TTL)
	markSnapshotStale(&arena, !arena.FetchedAt.IsZero() && opts.Now().Sub(arena.FetchedAt) > opts.TTL)
	sourceModels := evalsSourceModels(evalsSourceSet{
		llm: llm.Models, aa: aa.Models, writing: writing.Models, eqBench: eqBench.Models,
		fiction: fiction.Models, arena: arena.Models, ifeval: ifeval.Models,
		lfm: lfm.Models, qwen: qwen.Models, gptOSS: gptOSS.Models, lfmVL: lfmVL.Models,
		gemma: gemma.Models, lfm8: lfm8.Models,
	})
	models := mergeWritingEvidenceModels(mergeModels(llm.Models, aa.Models), writing.Models, eqBench.Models, fiction.Models, arena.Models, ifeval.Models)
	models = mergeOfficialCardModels(models, lfm.Models, qwen.Models, gptOSS.Models, lfmVL.Models, gemma.Models, lfm8.Models)
	writingCache := "cached"
	if writingErr != nil {
		writingCache = "unavailable"
	}
	eqBenchCache := "cached"
	if eqBenchErr != nil {
		eqBenchCache = "unavailable"
	}
	fictionCache := "cached"
	if fictionErr != nil {
		fictionCache = "unavailable"
	}
	arenaCache := "cached"
	if arenaErr != nil {
		arenaCache = "unavailable"
	}
	return Result{
		GeneratedAt: opts.Now().UTC(), Models: models, SourceModels: sourceModels, ReferenceModels: sourceNativeModels(llm.Models, aa.Models, writing.Models, eqBench.Models, fiction.Models, arena.Models, ifeval.Models, lfm.Models, qwen.Models, gptOSS.Models, lfmVL.Models, gemma.Models, lfm8.Models), AAVersion: aa.AAVersion,
		Sources: []SourceStatus{
			statusFor("LLM Stats", LLMStatsLeaderboardURL, llm, "cached", nil),
			frozenLLMStatsCohortStatus(frozenErr),
			statusFor("Artificial Analysis", ArtificialAnalysisURL, aa, "cached", err),
			statusFor("WritingBench", opts.WritingBenchURL, writing, writingCache, writingErr),
			statusFor("EQ-Bench Creative v3", opts.EQBenchCreativeURL, eqBench, eqBenchCache, eqBenchErr),
			statusFor("Lech Mazur Creative Story-Writing", opts.LechMazurWritingURL, fiction, fictionCache, fictionErr),
			statusFor("Arena Creative Writing", opts.ArenaCreativeURL, arena, arenaCache, arenaErr),
			statusFor("Official IFEval", ifeval.URL, ifeval, "frozen", nil),
			lfmStatus,
			qwenStatus,
			gptOSSStatus,
			lfmVLStatus,
			gemmaStatus,
			lfm8Status,
		},
	}, nil
}

type sourceModelGroup struct {
	sourceID string
	models   []Model
}

type evalsSourceSet struct {
	llm, aa, writing, eqBench, fiction, arena, ifeval []Model
	lfm, qwen, gptOSS, lfmVL, gemma, lfm8             []Model
}

func evalsSourceModels(sources evalsSourceSet) []SourceModel {
	return taggedSourceModels([]sourceModelGroup{
		{llmStatsSourceID, sources.llm},
		{artificialAnalysisSourceID, sources.aa},
		{writingBenchSourceID, sources.writing},
		{eqBenchCreativeSourceID, sources.eqBench},
		{WritingPrimaryID, sources.fiction},
		{ArenaCreativeSourceID, sources.arena},
		{ifevalOfficialSourceID, sources.ifeval},
		{lfm25SourceID, sources.lfm},
		{qwen38SourceID, sources.qwen},
		{gptOSSSourceID, sources.gptOSS},
		{lfm25VLSourceID, sources.lfmVL},
		{gemma4SourceID, sources.gemma},
		{lfm25A1BSourceID, sources.lfm8},
	})
}

func taggedSourceModels(groups []sourceModelGroup) []SourceModel {
	total := 0
	for _, group := range groups {
		total += len(group.models)
	}
	result := make([]SourceModel, 0, total)
	for _, group := range groups {
		for _, model := range group.models {
			if model.Benchmarks != nil {
				benchmarks := make(map[string]BenchmarkResult, len(model.Benchmarks))
				for name, observation := range model.Benchmarks {
					benchmarks[name] = observation
				}
				model.Benchmarks = benchmarks
			}
			result = append(result, SourceModel{SourceID: group.sourceID, Model: model})
		}
	}
	return result
}

func frozenLLMStatsCohortStatus(frozenErr error) SourceStatus {
	status := SourceStatus{
		Name:            "LLM Stats frozen cohorts",
		Cache:           "sealed",
		ContentSHA:      llmStatsFrozenCohortsArtifactDigest,
		Methodology:     "exact hash-verified normalized observation artifact",
		Observations:    llmStatsFrozenCohortObservationRows,
		RegistryVersion: SourceRegistryVersion,
		EvidenceGrade:   "graded_aggregator",
	}
	if frozenErr != nil {
		status.Cache = "unavailable"
		status.ContentSHA = ""
		status.Observations = 0
		status.Error = fmt.Sprintf("frozen LLM Stats cohorts unavailable: %v", frozenErr)
	}
	return status
}

// mergeRefreshedStatuses replaces cache-only statuses with refreshed ones by
// source name rather than positional offset, so adding or reordering sources
// can never silently swap a status onto the wrong row. It fails closed when a
// refreshed card has no matching row, which would otherwise drop the fetched
// provenance from the report.
func mergeRefreshedStatuses(result []SourceStatus, refreshed []SourceStatus) error {
	byName := make(map[string]int, len(result))
	for i := range result {
		byName[result[i].Name] = i
	}
	for i := range refreshed {
		index, ok := byName[refreshed[i].Name]
		if !ok {
			return fmt.Errorf("refreshed official model card status %q has no matching source row", refreshed[i].Name)
		}
		result[index] = refreshed[i]
	}
	return nil
}

func sourceNativeModels(sources ...[]Model) []Model {
	total := 0
	for _, source := range sources {
		total += len(source)
	}
	models := make([]Model, 0, total)
	for _, source := range sources {
		models = append(models, source...)
	}
	return models
}

// mergeOfficialCardModels reconciles only exact canonical keys. It deliberately
// avoids fuzzy model identity: an official card may replace a lower-authority
// benchmark result and retain it as a mirror, but a spelling similarity never
// creates a model merge.
func mergeOfficialCardModels(models []Model, cards ...[]Model) []Model {
	byKey := make(map[string]int, len(models))
	for i := range models {
		byKey[models[i].Key] = i
		if alias, ok := officialCardKeyAlias(models[i].Key); ok {
			byKey[alias] = i
		}
	}
	for _, card := range cards {
		for _, source := range card {
			if index, ok := byKey[source.Key]; ok {
				attachBenchmark(&models[index], source, IdentityMatchExact)
				continue
			}
			models = append(models, source)
			byKey[source.Key] = len(models) - 1
		}
	}
	return models
}

func markSnapshotStale(snapshot *sourceSnapshot, stale bool) {
	if snapshot == nil || !stale {
		return
	}
	for i := range snapshot.Models {
		snapshot.Models[i].EvidenceStale = true
		for name, result := range snapshot.Models[i].Benchmarks {
			result.Stale = true
			snapshot.Models[i].Benchmarks[name] = result
		}
	}
}

func (o *Options) applyDefaults() {
	if o.TTL <= 0 {
		o.TTL = DefaultTTL
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 45 * time.Second}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
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
		o.officialLFM8Fetcher = fetchOfficialLFM25A1B
	}
}
