package evals

import (
	"net/http"
	"time"
)

const (
	DefaultTTL             = 48 * time.Hour
	LLMStatsLeaderboardURL = "https://llm-stats.com/leaderboards/open-llm-leaderboard"
	ArtificialAnalysisURL  = "https://artificialanalysis.ai/"
)

type Options struct {
	CacheDir             string
	TTL                  time.Duration
	Refresh              bool
	RefreshOfficialCards bool
	Offline              bool
	AllowPartial         bool
	AAAPIKey             string
	Client               *http.Client
	LLMModelsURL         string
	LLMFullURL           string
	LLMIndexURL          string
	LLMBenchmarksURL     string
	IngestLLMBenchmarks  bool
	AAURL                string
	WritingBenchURL      string
	EQBenchCreativeURL   string
	OfficialLFMURL       string
	OfficialQwenURL      string
	OfficialGPTOSSURL    string
	OfficialLFMVLURL     string
	OfficialGemmaURL     string
	Now                  func() time.Time
}

type Result struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Models      []Model        `json:"models"`
	Sources     []SourceStatus `json:"sources"`
	AAVersion   float64        `json:"artificial_analysis_index_version,omitempty"`
	// ReferenceModels preserves source-native rows for drift calculations. It is
	// not report output and never participates in identity joins or scoring rows.
	ReferenceModels []Model `json:"-"`
}

type SourceStatus struct {
	Name            string    `json:"name"`
	URL             string    `json:"url"`
	FetchedAt       time.Time `json:"fetched_at,omitempty"`
	Cache           string    `json:"cache"` // fetched, fresh, cached, stale, frozen, or unavailable
	Models          int       `json:"models"`
	Version         string    `json:"version,omitempty"`
	CommitSHA       string    `json:"commit_sha,omitempty"`
	ContentSHA      string    `json:"content_sha256,omitempty"`
	Methodology     string    `json:"methodology,omitempty"`
	Observations    int       `json:"observations,omitempty"`
	RegistryVersion string    `json:"source_registry_version,omitempty"`
	EvidenceGrade   string    `json:"evidence_grade,omitempty"`
	Error           string    `json:"error,omitempty"`
}

type Model struct {
	Key           string                     `json:"key"`
	Name          string                     `json:"name"`
	Organization  string                     `json:"organization,omitempty"`
	IdentityMatch IdentityMatch              `json:"identity_match"`
	License       string                     `json:"license,omitempty"`
	Open          *bool                      `json:"open,omitempty"`
	Context       *int64                     `json:"context,omitempty"`
	LLMStats      *LLMStatsMetrics           `json:"llm_stats,omitempty"`
	AA            *ArtificialMetrics         `json:"artificial_analysis,omitempty"`
	Benchmarks    map[string]BenchmarkResult `json:"benchmarks,omitempty"`
	EvidenceStale bool                       `json:"-"`
}

// BenchmarkResult is a versioned source-native benchmark observation. It is
// never synthesized from local evaluations or from a composite index.
type BenchmarkResult struct {
	Score          *float64           `json:"score,omitempty"`
	Identity       IdentityMatch      `json:"identity_match,omitempty"`
	Version        string             `json:"version,omitempty"`
	URL            string             `json:"url,omitempty"`
	CommitSHA      string             `json:"commit_sha,omitempty"`
	ContentSHA     string             `json:"content_sha256,omitempty"`
	Method         string             `json:"methodology,omitempty"`
	Judge          string             `json:"judge_version,omitempty"`
	Stale          bool               `json:"-"`
	FetchedAt      time.Time          `json:"fetched_at,omitempty"`
	Details        map[string]float64 `json:"details,omitempty"`
	SourceClass    string             `json:"source_class,omitempty"`
	EvidenceGrade  string             `json:"evidence_grade,omitempty"`
	Unit           string             `json:"unit,omitempty"`
	Direction      string             `json:"direction,omitempty"`
	Cohort         string             `json:"cohort,omitempty"`
	SampleSize     int                `json:"sample_size,omitempty"`
	Locator        string             `json:"provenance_locator,omitempty"`
	SourceID       string             `json:"source_id,omitempty"`
	SourceRevision string             `json:"source_revision,omitempty"`
	Mirrors        []BenchmarkMirror  `json:"mirrors,omitempty"`
	Quarantined    bool               `json:"quarantined,omitempty"`
	Conflict       string             `json:"conflict,omitempty"`
}

// BenchmarkMirror preserves a deduplicated lower-precedence or corroborating
// copy without granting it another vote in the category scorer.
type BenchmarkMirror struct {
	SourceID       string `json:"source_id,omitempty"`
	SourceClass    string `json:"source_class,omitempty"`
	SourceRevision string `json:"source_revision,omitempty"`
	ContentSHA     string `json:"content_sha256,omitempty"`
	Locator        string `json:"provenance_locator,omitempty"`
}

// IdentityMatch describes whether a source row was joined to one unique row
// from the other source or explicitly projected from a reviewed upstream row.
// Ambiguous rows are deliberately kept separate.
type IdentityMatch string

const (
	IdentityMatchExact      IdentityMatch = "exact"
	IdentityMatchNormalized IdentityMatch = "normalized"
	IdentityMatchProjected  IdentityMatch = "projected"
	IdentityMatchUnmatched  IdentityMatch = "unmatched"
	IdentityMatchAmbiguous  IdentityMatch = "ambiguous"
)

type LLMStatsMetrics struct {
	InputPrice  *float64         `json:"input_price,omitempty"`
	OutputPrice *float64         `json:"output_price,omitempty"`
	Throughput  *float64         `json:"throughput,omitempty"`
	Latency     *float64         `json:"latency,omitempty"`
	GPQA        *float64         `json:"gpqa,omitempty"`
	SWEVerified *float64         `json:"swe_bench_verified,omitempty"`
	SWEPro      *float64         `json:"swe_bench_pro,omitempty"`
	SciCode     *float64         `json:"scicode,omitempty"`
	MCPAtlas    *float64         `json:"mcp_atlas,omitempty"`
	Indexes     map[string]Index `json:"indexes,omitempty"`
}

type Index struct {
	Conservative float64 `json:"conservative"`
	Mu           float64 `json:"mu"`
	Sigma        float64 `json:"sigma"`
	Rank         int     `json:"rank"`
	GamesPlayed  int     `json:"games_played"`
}

type ArtificialMetrics struct {
	sourceName         string
	sourceOrganization string
	ID                 string   `json:"id,omitempty"`
	Slug               string   `json:"slug,omitempty"`
	Intelligence       *float64 `json:"intelligence,omitempty"`
	Coding             *float64 `json:"coding,omitempty"`
	Agentic            *float64 `json:"agentic,omitempty"`
	InputPrice         *float64 `json:"input_price,omitempty"`
	OutputPrice        *float64 `json:"output_price,omitempty"`
	OutputTokensPS     *float64 `json:"output_tokens_per_second,omitempty"`
	TTFTSeconds        *float64 `json:"time_to_first_token_seconds,omitempty"`
	E2ESeconds         *float64 `json:"end_to_end_seconds,omitempty"`
}

func boolPtr(v bool) *bool { return &v }
