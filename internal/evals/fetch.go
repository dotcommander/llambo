package evals

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"
)

// Repeated string literals consolidated by goconst.
const (
	cached1          = "cached"
	gradedAggregator = "graded_aggregator"
	stale1           = "stale"
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

// Fetch loads evaluation sources using the requested cache and refresh policy.
func Fetch(ctx context.Context, opts Options) (Result, error) {
	return fetchWithOfficialCardFetchers(ctx, opts, officialCardFetchers{})
}

func fetchWithOfficialCardFetchers(ctx context.Context, opts Options, fetchers officialCardFetchers) (Result, error) {
	opts.applyDefaults()
	if opts.Refresh && opts.RefreshOfficialCards {
		return Result{}, errors.New("global refresh and official-card refresh cannot be combined")
	}
	if opts.Offline && opts.RefreshOfficialCards {
		return Result{}, errors.New("offline and official-card refresh cannot be combined")
	}
	ifeval, err := loadOfficialIFEvalSnapshot()
	if err != nil {
		return Result{}, err
	}
	if opts.Offline {
		return fetchCachedOnly(opts, ifeval)
	}
	if opts.RefreshOfficialCards {
		return fetchRefreshedOfficialCards(ctx, opts, ifeval, fetchers)
	}
	return fetchOnlineSources(ctx, opts, ifeval, fetchers)
}

var (
	fetchOfficialLFM25Source                   officialCardFetcher
	fetchOfficialQwen38Source                  officialCardFetcher
	fetchOfficialGPTOSSSource                  officialCardFetcher
	fetchOfficialLFMVLSource                   officialCardFetcher
	fetchOfficialGemmaSource                   officialCardFetcher
	loadFrozenLLMStatsCachedObservationsSource = loadFrozenLLMStatsCachedObservations
)

func officialCardFetcherForSpec(spec officialCardSpec) officialCardFetcher {
	return func(ctx context.Context, opts Options) (sourceSnapshot, error) {
		return fetchOfficialCard(ctx, opts, spec)
	}
}

func refreshOfficialModelCardsWithFetchers(ctx context.Context, opts Options, fetchers officialCardFetchers) ([]SourceStatus, error) {
	refresh := opts
	refresh.Refresh = true
	statuses := make([]SourceStatus, 0, len(officialCardDescriptors))
	var cardErrs []error
	for i, descriptor := range officialCardDescriptors {
		fetcher := fetchers.resolve(opts, i)
		snapshot, status, err := loadSource(ctx, refresh, descriptor.cacheName, func(ctx context.Context) (sourceSnapshot, error) {
			return fetcher(ctx, refresh)
		})
		if err == nil && status.Cache != "fetched" {
			err = fmt.Errorf("cache status %q", status.Cache)
			if status.Error != "" {
				err = fmt.Errorf("cache status %q: %s", status.Cache, status.Error)
			}
		}
		if err != nil {
			cardErrs = append(cardErrs, fmt.Errorf("refresh official model card %q: %w", descriptor.cacheName, err))
			continue
		}
		if err := validateOfficialCardSnapshot(descriptor.cacheName, snapshot); err != nil {
			cardErrs = append(cardErrs, fmt.Errorf("refresh official model card %q: %w", descriptor.cacheName, err))
			continue
		}
		statuses = append(statuses, status)
	}
	return statuses, errors.Join(cardErrs...)
}

func fetchCachedOnly(opts Options, ifeval sourceSnapshot) (Result, error) {
	return fetchCachedOnlyWithFrozenLoader(opts, ifeval, loadFrozenLLMStatsCachedObservationsSource)
}

func fetchCachedOnlyWithFrozenLoader(opts Options, ifeval sourceSnapshot, loader func(string) ([]Observation, error)) (Result, error) {
	var cards [officialCardCount]sourceSnapshot
	var cardStatuses [officialCardCount]SourceStatus
	for i, descriptor := range officialCardDescriptors {
		cards[i], cardStatuses[i] = readOptionalOfficialSnapshot(opts, descriptor.cacheName)
	}
	lfm, qwen, gptOSS, lfmVL, gemma, lfm8 := cards[officialLFM25Card], cards[officialQwen38Card], cards[officialGPTOSSCard], cards[officialLFMVLCard], cards[officialGemma4Card], cards[officialLFM25A1BCard]
	lfmStatus, qwenStatus, gptOSSStatus, lfmVLStatus, gemmaStatus, lfm8Status := cardStatuses[officialLFM25Card], cardStatuses[officialQwen38Card], cardStatuses[officialGPTOSSCard], cardStatuses[officialLFMVLCard], cardStatuses[officialGemma4Card], cardStatuses[officialLFM25A1BCard]
	llm, err := readSnapshot(filepath.Join(opts.CacheDir, "llm-stats.json"))
	if err != nil {
		return Result{}, fmt.Errorf("read cached LLM Stats: %w", err)
	}
	frozenErr := prepareFrozenLLMStatsCohortResultsWithLoader(llm.Models, opts.CacheDir, loader)
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
	writingCache := cached1
	if writingErr != nil {
		writingCache = unavailable1
	}
	eqBenchCache := cached1
	if eqBenchErr != nil {
		eqBenchCache = unavailable1
	}
	fictionCache := cached1
	if fictionErr != nil {
		fictionCache = unavailable1
	}
	arenaCache := cached1
	if arenaErr != nil {
		arenaCache = unavailable1
	}
	return Result{
		GeneratedAt: opts.Now().UTC(), Models: models, SourceModels: sourceModels, ReferenceModels: sourceNativeModels(llm.Models, aa.Models, writing.Models, eqBench.Models, fiction.Models, arena.Models, ifeval.Models, lfm.Models, qwen.Models, gptOSS.Models, lfmVL.Models, gemma.Models, lfm8.Models), AAVersion: aa.AAVersion,
		Sources: []SourceStatus{
			statusFor("LLM Stats", LLMStatsLeaderboardURL, llm, cached1, nil),
			frozenLLMStatsCohortStatus(frozenErr),
			statusFor(artificialAnalysisName, ArtificialAnalysisURL, aa, cached1, err),
			statusFor(writingBench1, opts.WritingBenchURL, writing, writingCache, writingErr),
			statusFor(eQBenchCreativeV3, opts.EQBenchCreativeURL, eqBench, eqBenchCache, eqBenchErr),
			statusFor(lechMazurCreativeStoryWriting, opts.LechMazurWritingURL, fiction, fictionCache, fictionErr),
			statusFor(arenaCreativeWriting, opts.ArenaCreativeURL, arena, arenaCache, arenaErr),
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
		EvidenceGrade:   gradedAggregator,
	}
	if frozenErr != nil {
		status.Cache = unavailable1
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
	o.applySourceDefaults()
	o.applyOfficialCardDefaults()
}
