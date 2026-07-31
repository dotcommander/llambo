package gateway

import (
	"encoding/json"
	"fmt"
	"time"
)

// OpenAI-compatible request/response types

// ChatCompletionRequest matches OpenAI's chat completion request format
type ChatCompletionRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	TopP           *float64        `json:"top_p,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Stream         bool            `json:"stream"`
}

// ResponseFormat captures OpenAI-compatible structured response requests.
type ResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionResponse matches OpenAI's chat completion response format
type ChatCompletionResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []Choice    `json:"choices"`
	Usage   *Usage      `json:"usage,omitempty"`
	XLlambo *LlamboMeta `json:"x_llambo,omitempty"`
}

// Choice represents a completion choice
type Choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Delta   `json:"delta,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

// Delta represents streaming content delta
type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// Usage represents token usage
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// LlamboMeta contains llambo-specific metadata
// Named with "X" prefix in JSON (x_llambo) following OpenAI convention
// for extension/custom fields in compatible APIs
type LlamboMeta struct {
	Backend          string  `json:"backend"`
	Model            string  `json:"model,omitempty"`
	DurationMs       int64   `json:"duration_ms"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	TotalTokens      int     `json:"total_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	RoutingMode      string  `json:"routing_mode,omitempty"`
	RoutingIntent    string  `json:"routing_intent,omitempty"`
	RoutingReason    string  `json:"routing_reason,omitempty"`
}

// EmbeddingRequest matches OpenAI's embedding request format
type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

func (r *EmbeddingRequest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Model string          `json:"model"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.Model = raw.Model
	if len(raw.Input) == 0 || string(raw.Input) == "null" {
		r.Input = nil
		return nil
	}

	var single string
	if err := json.Unmarshal(raw.Input, &single); err == nil {
		r.Input = []string{single}
		return nil
	}

	var multiple []string
	if err := json.Unmarshal(raw.Input, &multiple); err == nil {
		r.Input = multiple
		return nil
	}

	return fmt.Errorf("input must be a string or array of strings")
}

// EmbeddingResponse matches OpenAI's embedding response format
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  Usage           `json:"usage"`
}

// EmbeddingData represents a single embedding
type EmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// ModelsResponse matches OpenAI's models list response
type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// ModelInfo represents a single model
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// Job queue types

// CreateJobRequest submits a batch of requests for parallel processing
type CreateJobRequest struct {
	Requests     []JobRequest `json:"requests"`
	SystemPrompt string       `json:"system_prompt,omitempty"`
	Model        string       `json:"model,omitempty"`
}

// JobRequest represents a single request within a batch job
type JobRequest struct {
	ID       string    `json:"id"`
	Messages []Message `json:"messages"`
}

// JobResponse represents job status and results
type JobResponse struct {
	JobID     string      `json:"job_id"`
	Status    string      `json:"status"` // pending, processing, completed, failed, cancelled
	Total     int         `json:"total"`
	Completed int         `json:"completed"`
	Failed    int         `json:"failed"`
	Results   []JobResult `json:"results,omitempty"`
	CreatedAt int64       `json:"created_at"`
	UpdatedAt int64       `json:"updated_at,omitempty"`
}

// JobResult represents the result of a single request within a job
type JobResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"` // pending, processing, completed, failed
	Content    string `json:"content,omitempty"`
	Error      string `json:"error,omitempty"`
	Backend    string `json:"backend,omitempty"`
	Model      string `json:"model,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// Health and status types

// HealthResponse represents gateway health status
type HealthResponse struct {
	Status        string                           `json:"status"` // healthy, degraded, unhealthy
	UptimeSeconds int64                            `json:"uptime_seconds"`
	Backends      map[string]BackendHealthResponse `json:"backends"`
}

// BackendHealthResponse represents a single backend's health in API responses.
// This is distinct from providers.BackendHealth which is used internally.
type BackendHealthResponse struct {
	Healthy    bool       `json:"healthy"`
	Failures   int        `json:"failures"`
	DisabledAt *time.Time `json:"disabled_at,omitempty"`
}

// ProvidersResponse lists all configured providers
type ProvidersResponse struct {
	Providers []ProviderInfo `json:"providers"`
}

// ProviderInfo represents a single provider's configuration
type ProviderInfo struct {
	Name     string `json:"name"`
	Model    string `json:"model"`
	Enabled  bool   `json:"enabled"`
	Healthy  bool   `json:"healthy"`
	Priority int    `json:"priority"`
	Workers  int    `json:"workers"`
}

// ErrorResponse represents an API error
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error information
type ErrorDetail struct {
	Message          string `json:"message"`
	Type             string `json:"type"`
	Code             string `json:"code,omitempty"`
	FinishReason     string `json:"finish_reason,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	TotalTokens      int    `json:"total_tokens,omitempty"`
}

// StatsResponse represents aggregated provider statistics
type StatsResponse struct {
	Providers map[string]ProviderStatsResponse `json:"providers"`
	Total     ProviderStatsResponse            `json:"total"`
	Routing   map[string]RoutingStatsResponse  `json:"routing,omitempty"`
}

// ProviderStatsResponse represents stats for a single provider
type ProviderStatsResponse struct {
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
}

// RoutingStatsResponse represents rolling intelligent-routing metrics.
type RoutingStatsResponse struct {
	Requests       int64   `json:"requests"`
	Successes      int64   `json:"successes"`
	Failures       int64   `json:"failures"`
	Timeouts       int64   `json:"timeouts"`
	AvgLatencyMs   int64   `json:"avg_latency_ms"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	DailyWindow    string  `json:"daily_window,omitempty"`
	DailyRequests  int64   `json:"daily_requests"`
	DailyTokens    int64   `json:"daily_tokens"`
	DailyCostUSD   float64 `json:"daily_cost_usd"`
	DailyLimitReq  int64   `json:"daily_limit_requests,omitempty"`
	DailyLimitTok  int64   `json:"daily_limit_tokens,omitempty"`
	DailyLimitCost float64 `json:"daily_limit_cost_usd,omitempty"`
	DailyExhausted bool    `json:"daily_exhausted"`
	LastUpdatedISO string  `json:"last_updated_iso,omitempty"`
}
