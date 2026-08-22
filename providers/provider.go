package providers

import (
	"context"
	"encoding/json"
	"os"
	"strings"
)

// Provider interface for AI backends. Chat requires a non-nil ctx.
type Provider interface {
	Name() string
	Chat(ctx context.Context, systemPrompt, userContent string) (string, error)
	MaxTokens() int
}

// LLMCost contains cost information for a request
type LLMCost struct {
	TotalCost float64
}

// LLMUsage contains token usage and cost information
type LLMUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheReadTokens  int
	CacheWriteTokens int
	ReasoningTokens  int
	Cost             *LLMCost
}

// ChatResult contains response with metadata
type ChatResult struct {
	Content  string
	Provider string    // which provider handled the request
	Model    string    // which model was used
	Usage    *LLMUsage // token usage and cost (may be nil)
	// FinishReason carries provider completion reason when available
	// (e.g., stop, length, content_filter, tool_calls).
	FinishReason string
	ToolCalls    []ToolCall
	Route        *RouteDecision
}

// ToolCall captures model-emitted tool call metadata from provider responses.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ToolCallDelta represents incremental tool-call data while streaming.
type ToolCallDelta struct {
	Index          int
	ID             string
	Name           string
	ArgumentsDelta string
}

// ChatStreamChunk represents one streamed delta chunk.
type ChatStreamChunk struct {
	ContentDelta string
	ToolDeltas   []ToolCallDelta
	FinishReason string
	Usage        *LLMUsage
	// Provider and Model identify the backend that emitted this chunk. They
	// are additive so existing stream consumers remain source-compatible.
	Provider string
	Model    string
}

// ChatStreamHandler is invoked for each streamed delta chunk.
type ChatStreamHandler func(chunk ChatStreamChunk) error

// ProviderWithInfo extends Provider with detailed response info.
// ChatWithInfo requires a non-nil ctx.
type ProviderWithInfo interface {
	Provider
	ChatWithInfo(ctx context.Context, systemPrompt, userContent string) (ChatResult, error)
}

// ChatStreamProvider is implemented by providers that support streaming deltas.
type ChatStreamProvider interface {
	ChatStreamWithInfoContext(ctx context.Context, systemPrompt, userContent string, onChunk ChatStreamHandler) (ChatResult, error)
}

// Default values for Config fields
const (
	DefaultWorkers      = 2
	DefaultProviderType = "openai"
	DefaultRequiresKey  = true
)

// Config holds provider configuration
type Config struct {
	APIKey       string            `json:"api_key,omitempty"`
	APIKeys      []string          `json:"api_keys,omitempty"` // multiple API keys for rotation on 429
	BaseURL      string            `json:"base_url"`
	Model        string            `json:"model"`
	Models       []string          `json:"models,omitempty"` // optional model variants for benchmarking/listing
	MaxTokens    int               `json:"max_tokens,omitempty"`
	Temperature  float64           `json:"temperature,omitempty"`
	Enabled      bool              `json:"enabled"`
	Workers      int               `json:"workers,omitempty"`       // concurrent requests per provider (default 2)
	Priority     int               `json:"priority,omitempty"`      // load balancing order (lower = higher priority)
	ProviderType string            `json:"provider_type,omitempty"` // "openai", "openrouter", "gemini" - defaults to "openai"
	EnvVar       string            `json:"env_var,omitempty"`       // env var for API key (defaults to NAME_API_KEY)
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
	ExtraBody    map[string]any    `json:"extra_body,omitempty"` // extra JSON fields for request body (e.g., thinking for z.ai)
	// ExtraBodyByModel allows per-model JSON overrides on top of ExtraBody.
	// Keyed by full model id (e.g., "gpt-5-mini", "qwen/qwen3-32b").
	// Per-model values override provider-level ExtraBody on the same key path
	// because the per-model loop runs after the provider loop (last-write-wins).
	// Keys within ExtraBodyByModel[model] should be distinct top-level paths.
	ExtraBodyByModel  map[string]map[string]any `json:"extra_body_by_model,omitempty"`
	RequiresKey       bool                      `json:"requires_key,omitempty"`   // defaults to true; set false for local providers
	APIPath           string                    `json:"api_path,omitempty"`       // legacy compatibility field; serving path ignores it
	ModelsDevKey      string                    `json:"models_dev_key,omitempty"` // models.dev top-level provider key for pricing (default: provider name)
	Capabilities      []string                  `json:"capabilities,omitempty"`   // optional tags: chat, code, extraction, long_context, embeddings
	Quality           float64                   `json:"quality,omitempty"`        // relative quality score (0..1, default 0.5)
	InputCostPM       float64                   `json:"input_cost_per_1m,omitempty"`
	OutputCostPM      float64                   `json:"output_cost_per_1m,omitempty"`
	ExpectedLatencyMs int                       `json:"expected_latency_ms,omitempty"`
	ContextLength     int                       `json:"context_length,omitempty"` // model context window in tokens; 0 = unknown (precheck never drops)
}

func (c *Config) UnmarshalJSON(data []byte) error {
	type configAlias Config
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	alias := configAlias{RequiresKey: DefaultRequiresKey}
	if _, ok := raw["requires_key"]; ok {
		alias.RequiresKey = false
	}
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*c = Config(alias)
	return nil
}

// ApplyDefaults sets default values for unset fields.
// Call this after unmarshaling config from JSON.
func (c *Config) ApplyDefaults() {
	if c.Workers <= 0 {
		c.Workers = DefaultWorkers
	}
	if c.ProviderType == "" {
		c.ProviderType = DefaultProviderType
	}
	if c.Quality <= 0 {
		c.Quality = 0.5
	}
}

// GetWorkers returns workers count.
// Note: Call ApplyDefaults() first to ensure defaults are set.
func (c Config) GetWorkers() int {
	if c.Workers <= 0 {
		return DefaultWorkers
	}
	return c.Workers
}

// GetProviderType returns the provider type.
// Note: Call ApplyDefaults() first to ensure defaults are set.
func (c Config) GetProviderType() string {
	if c.ProviderType == "" {
		return DefaultProviderType
	}
	return c.ProviderType
}

// IsGeminiProvider returns true when config identifies a native Gemini provider.
func IsGeminiProvider(cfg Config) bool {
	return cfg.ProviderType == "gemini"
}

// GetRequiresKey returns whether API key is required.
func (c Config) GetRequiresKey() bool {
	return c.RequiresKey
}

// NeedsBaseURL returns true if provider needs custom BaseURL (OpenAI-compatible with custom endpoint)
func (c Config) NeedsBaseURL() bool {
	// Native providers don't need BaseURL override
	switch c.GetProviderType() {
	case "openrouter", "gemini":
		return false
	default:
		// OpenAI-compatible providers need BaseURL if it's not the default OpenAI URL
		return c.BaseURL != "" && c.BaseURL != "https://api.openai.com/v1"
	}
}

// GetAPIKey retrieves API key from env or config
func GetAPIKey(name string, cfg Config) string {
	if cfg.APIKey != "" {
		return cfg.APIKey
	}

	// Use config's env_var if specified, otherwise default to NAME_API_KEY
	envVar := cfg.EnvVar
	if envVar == "" {
		envVar = strings.ToUpper(name) + "_API_KEY"
	}
	return os.Getenv(envVar)
}

// GetAPIKeys returns all available API keys for a provider.
// Returns keys from api_keys array first, then single api_key, then env var.
func GetAPIKeys(name string, cfg Config) []string {
	var keys []string

	// Add keys from api_keys array
	for _, k := range cfg.APIKeys {
		if k != "" {
			keys = append(keys, k)
		}
	}

	// Add single api_key if set and not already in array
	if cfg.APIKey != "" {
		found := false
		for _, k := range keys {
			if k == cfg.APIKey {
				found = true
				break
			}
		}
		if !found {
			keys = append(keys, cfg.APIKey)
		}
	}

	// Add env var key if not already present
	envKey := ""
	envVar := cfg.EnvVar
	if envVar == "" {
		envVar = strings.ToUpper(name) + "_API_KEY"
	}
	envKey = os.Getenv(envVar)
	if envKey != "" {
		found := false
		for _, k := range keys {
			if k == envKey {
				found = true
				break
			}
		}
		if !found {
			keys = append(keys, envKey)
		}
	}

	return keys
}

// ChatProvider is the interface for chat completion providers with failover support.
// ChatWithInfo requires a non-nil ctx.
type ChatProvider interface {
	Provider
	ChatWithInfo(ctx context.Context, systemPrompt, userContent string) (ChatResult, error)
	ChatWithInfoContext(ctx context.Context, systemPrompt, userContent string) (ChatResult, error)
	GetOpenAIClients() *OpenAIClients
	Shutdown()
}

// Ensure OpenAIProvider implements ChatProvider
var _ ChatProvider = (*OpenAIProvider)(nil)
