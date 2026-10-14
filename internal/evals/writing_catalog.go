package evals

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Repeated string literals consolidated by goconst.
const (
	apacheX2X0         = "Apache-2.0"
	candidateVariant   = "candidate variant"
	high1              = "high"
	mIT                = "MIT"
	primaryLeaderboard = "primary leaderboard"
	qwen1              = "Qwen"
	variant1           = "variant"
)

const lechMazurWritingStylesURL = "https://github.com/lechmazur/writing_styles"

const (
	// WritingCatalogVersion is the persisted writing catalog schema version.
	WritingCatalogVersion = 3
	// WritingPrimaryID identifies the primary writing leaderboard.
	WritingPrimaryID = "lechmazur-writing"
	// WritingPrimaryURL locates the primary writing leaderboard artifact.
	WritingPrimaryURL    = "https://raw.githubusercontent.com/lechmazur/writing/main/README.md"
	maxWritingSourceBody = 32 << 20
)

// WritingCatalog describes writing-focused evaluation sources without mixing
// their source-native scores into the LLAMBO-2 report.
// WritingCatalog is the sealed writing benchmark and open-model catalog.
type WritingCatalog struct {
	GeneratedAt          time.Time                    `json:"generated_at"`
	RegistryVersion      int                          `json:"registry_version"`
	Benchmarks           []WritingBenchmark           `json:"benchmarks"`
	OpenModels           []WritingOpenModel           `json:"open_models"`
	SourceChecks         []WritingSourceStatus        `json:"source_checks,omitempty"`
	OpenModelChecks      []WritingModelStatus         `json:"open_model_checks,omitempty"`
	OpenModelCoverage    []WritingModelCoverage       `json:"open_model_coverage,omitempty"`
	Leaderboards         []WritingLeaderboard         `json:"leaderboards,omitempty"`
	PromptRecords        []WritingPromptRecord        `json:"prompt_records,omitempty"`
	OpenModelDiscovery   *WritingOpenModelDiscovery   `json:"open_model_discovery,omitempty"`
	DiscoveredOpenModels []WritingDiscoveredOpenModel `json:"discovered_open_models,omitempty"`
}

// WritingBenchmark describes one writing benchmark source.
type WritingBenchmark struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	DataURL       string `json:"data_url,omitempty"`
	PromptURL     string `json:"prompt_url,omitempty"`
	PromptFormat  string `json:"prompt_format,omitempty"`
	PromptCount   int    `json:"prompt_count,omitempty"`
	RunMode       string `json:"run_mode,omitempty"`
	RunNotes      string `json:"run_notes,omitempty"`
	Focus         string `json:"focus"`
	Scoring       string `json:"scoring"`
	ScrapeMethod  string `json:"scrape_method"`
	UpdateCadence string `json:"update_cadence"`
	Notes         string `json:"notes,omitempty"`
}

// WritingOpenModel is a reviewed open-weight candidate or a model already
// represented by one of the public writing leaderboards. Coverage is kept
// explicit because public weights and leaderboard coverage are independent.
type WritingOpenModel struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Provider             string   `json:"provider"`
	HuggingFaceURL       string   `json:"huggingface_url"`
	License              string   `json:"license"`
	Coverage             string   `json:"coverage"`
	Priority             string   `json:"priority"`
	LeaderboardAliases   []string `json:"leaderboard_aliases,omitempty"`
	LeaderboardMatchType string   `json:"leaderboard_match_type,omitempty"`
	Notes                string   `json:"notes,omitempty"`
}

// WritingLeaderboard is one fetched public leaderboard.
type WritingLeaderboard struct {
	BenchmarkID string                  `json:"benchmark_id"`
	SourceURL   string                  `json:"source_url"`
	FetchedAt   time.Time               `json:"fetched_at"`
	Rows        []WritingLeaderboardRow `json:"rows"`
	Error       string                  `json:"error,omitempty"`
}

// WritingLeaderboardRow is one leaderboard entry with uncertainty bounds.
type WritingLeaderboardRow struct {
	Rank      int     `json:"rank"`
	Model     string  `json:"model"`
	Score     float64 `json:"comparison_score"`
	WinChance float64 `json:"win_chance_percent"`
	Lower     float64 `json:"uncertainty_lower"`
	Upper     float64 `json:"uncertainty_upper"`
}

// WritingCatalogOptions controls catalog fetching and validation depth.
type WritingCatalogOptions struct {
	Client               *http.Client
	URL                  string
	Now                  func() time.Time
	ValidateSources      bool
	ValidateModels       bool
	IncludePromptRecords bool
	DiscoverOpenModels   bool
	DiscoverLimit        int
	DiscoveryURL         string
	SourceURLs           map[string]string
	ModelURLs            map[string]string
}

// FetchWritingCatalog assembles the writing catalog from reviewed sources.
func FetchWritingCatalog(ctx context.Context, opts WritingCatalogOptions) (WritingCatalog, error) {
	if opts.IncludePromptRecords {
		opts.ValidateSources = true
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	url := opts.URL
	if url == "" {
		url = WritingPrimaryURL
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	fetchedAt := now().UTC()
	catalog := DefaultWritingCatalog(fetchedAt)
	artifact, err := fetchWritingArtifact(ctx, client, url, fetchedAt)
	if err != nil {
		return WritingCatalog{}, fmt.Errorf("fetch writing leaderboard: %w", err)
	}
	rows, err := ParseWritingLeaderboard(string(artifact.Body))
	if err != nil {
		return WritingCatalog{}, err
	}
	catalog.SourceChecks = append(catalog.SourceChecks, WritingSourceStatus{
		BenchmarkID: WritingPrimaryID,
		SourceURL:   url,
		FetchedAt:   artifact.FetchedAt,
		Status:      available1,
		HTTPStatus:  artifact.HTTPStatus,
		ContentType: artifact.ContentType,
		Bytes:       artifact.Bytes,
		Records:     len(rows),
	})
	catalog.Leaderboards = []WritingLeaderboard{{
		BenchmarkID: WritingPrimaryID,
		SourceURL:   url,
		FetchedAt:   fetchedAt,
		Rows:        rows,
	}}
	catalog.OpenModelCoverage = buildWritingModelCoverage(catalog.OpenModels, rows)
	if opts.ValidateSources {
		checks, promptRecords := fetchWritingSourceChecks(ctx, client, catalog, opts, fetchedAt)
		catalog.SourceChecks = append(catalog.SourceChecks, checks...)
		if opts.IncludePromptRecords {
			catalog.PromptRecords = promptRecords
		}
		applyWritingPromptCounts(&catalog)
	}
	if opts.ValidateModels {
		catalog.OpenModelChecks = fetchWritingModelChecks(ctx, client, catalog.OpenModels, opts, fetchedAt)
	}
	if opts.DiscoverOpenModels {
		discovery, candidates := fetchWritingOpenModelDiscovery(ctx, client, catalog, opts, fetchedAt)
		catalog.OpenModelDiscovery = &discovery
		catalog.DiscoveredOpenModels = candidates
	}
	return catalog, nil
}
