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
	CacheDir     string
	TTL          time.Duration
	Refresh      bool
	Offline      bool
	AllowPartial bool
	AAAPIKey     string
	Client       *http.Client
	LLMModelsURL string
	LLMFullURL   string
	LLMIndexURL  string
	AAURL        string
	Now          func() time.Time
}

type Result struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Models      []Model        `json:"models"`
	Sources     []SourceStatus `json:"sources"`
	AAVersion   float64        `json:"artificial_analysis_index_version,omitempty"`
}

type SourceStatus struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	Cache     string    `json:"cache"` // fetched, fresh, cached, stale, or unavailable
	Models    int       `json:"models"`
	Error     string    `json:"error,omitempty"`
}

type Model struct {
	Key           string             `json:"key"`
	Name          string             `json:"name"`
	Organization  string             `json:"organization,omitempty"`
	IdentityMatch IdentityMatch      `json:"identity_match"`
	License       string             `json:"license,omitempty"`
	Open          *bool              `json:"open,omitempty"`
	Context       *int64             `json:"context,omitempty"`
	LLMStats      *LLMStatsMetrics   `json:"llm_stats,omitempty"`
	AA            *ArtificialMetrics `json:"artificial_analysis,omitempty"`
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
