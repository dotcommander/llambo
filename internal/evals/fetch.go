package evals

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"
)

const maxSourceBody = 32 << 20

type sourceSnapshot struct {
	FetchedAt time.Time `json:"fetched_at"`
	Models    []Model   `json:"models"`
	AAVersion float64   `json:"aa_version,omitempty"`
}

func Fetch(ctx context.Context, opts Options) (Result, error) {
	opts.applyDefaults()
	if opts.Offline {
		return fetchCachedOnly(opts)
	}
	llm, llmStatus, err := loadSource(ctx, opts, "llm-stats", func(ctx context.Context) (sourceSnapshot, error) {
		return fetchLLMStats(ctx, opts)
	})
	if err != nil {
		return Result{}, err
	}
	aa, aaStatus, aaErr := loadArtificialAnalysis(ctx, opts)
	if aaErr != nil && !opts.AllowPartial {
		return Result{}, aaErr
	}

	models := mergeModels(llm.Models, aa.Models)
	return Result{
		GeneratedAt: opts.Now().UTC(),
		Models:      models,
		Sources:     []SourceStatus{llmStatus, aaStatus},
		AAVersion:   aa.AAVersion,
	}, nil
}

func fetchCachedOnly(opts Options) (Result, error) {
	llm, err := readSnapshot(filepath.Join(opts.CacheDir, "llm-stats.json"))
	if err != nil {
		return Result{}, fmt.Errorf("read cached LLM Stats: %w", err)
	}
	aa, err := readSnapshot(filepath.Join(opts.CacheDir, "artificial-analysis.json"))
	if err != nil && !opts.AllowPartial {
		return Result{}, fmt.Errorf("read cached Artificial Analysis: %w", err)
	}
	models := mergeModels(llm.Models, aa.Models)
	return Result{
		GeneratedAt: opts.Now().UTC(), Models: models, AAVersion: aa.AAVersion,
		Sources: []SourceStatus{
			statusFor("LLM Stats", LLMStatsLeaderboardURL, llm, "cached", nil),
			statusFor("Artificial Analysis", ArtificialAnalysisURL, aa, "cached", err),
		},
	}, nil
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
	if o.AAURL == "" {
		o.AAURL = "https://artificialanalysis.ai/api/v2/language/models/free"
	}
}
