package providers

import (
	"context"
	"fmt"

	whopenai "github.com/garyblankenship/wormhole/v3/providers/openai"
	"github.com/garyblankenship/wormhole/v3/types"
)

// EmbeddingProvider generates vector embeddings for text
type EmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
	ModelName() string
	Close()
}

// Ensure OpenAIEmbedding implements EmbeddingProvider
var _ EmbeddingProvider = (*OpenAIEmbedding)(nil)

// Default values for EmbedConfig fields
const (
	DefaultEmbedModel      = "text-embedding-3-small"
	DefaultEmbedDimensions = 1536
	DefaultEmbedBatchSize  = 100
)

// EmbedConfig holds embedding-specific configuration
type EmbedConfig struct {
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	BatchSize  int    `json:"batch_size"`
}

// ApplyDefaults sets default values for unset fields.
// Call this after unmarshaling config from JSON.
func (c *EmbedConfig) ApplyDefaults() {
	if c.Model == "" {
		c.Model = DefaultEmbedModel
	}
	if c.Dimensions == 0 {
		c.Dimensions = DefaultEmbedDimensions
	}
	if c.BatchSize == 0 {
		c.BatchSize = DefaultEmbedBatchSize
	}
}

// DefaultEmbedConfig returns sensible defaults
func DefaultEmbedConfig() EmbedConfig {
	cfg := EmbedConfig{}
	cfg.ApplyDefaults()
	return cfg
}

// OpenAIEmbedding implements EmbeddingProvider using a wormhole openai provider
type OpenAIEmbedding struct {
	provider   *whopenai.Provider
	model      string
	dimensions int
	batchSize  int
}

// NewOpenAIEmbedding creates an embedding provider from global config
func NewOpenAIEmbedding() (*OpenAIEmbedding, error) {
	globalCfg, err := LoadGlobalConfig()
	if err != nil {
		// Use defaults if no config found
		return NewOpenAIEmbeddingWithConfig(EmbedConfig{}, nil)
	}

	embedCfg := globalCfg.Embed
	if embedCfg.Model == "" {
		embedCfg = DefaultEmbedConfig()
	}

	return NewOpenAIEmbeddingWithConfig(embedCfg, globalCfg.Providers)
}

// selectEmbeddingProvider finds the best provider for embeddings.
// Priority: openai/openai-type first, then any enabled provider, then defaults.
func selectEmbeddingProvider(configs map[string]Config) (Config, string) {
	entries := FilterEnabledProviders(configs)

	// Try openai or openai-type provider first
	for _, entry := range entries {
		if entry.Name == "openai" || entry.Config.ProviderType == "openai" {
			return entry.Config, entry.Name
		}
	}

	// Fallback to any enabled provider
	if len(entries) > 0 {
		return entries[0].Config, entries[0].Name
	}

	// Return defaults
	return Config{
		BaseURL:      "https://api.openai.com",
		ProviderType: "openai",
		Enabled:      true,
	}, "openai"
}

// NewOpenAIEmbeddingWithConfig creates an embedding provider with explicit config
func NewOpenAIEmbeddingWithConfig(embedCfg EmbedConfig, providerConfigs map[string]Config) (*OpenAIEmbedding, error) {
	embedCfg.ApplyDefaults()

	selectedConfig, selectedName := selectEmbeddingProvider(providerConfigs)

	apiKey := GetAPIKey(selectedName, selectedConfig)
	if apiKey == "" && selectedConfig.GetRequiresKey() {
		return nil, fmt.Errorf("embedding client init for %s: no API key", selectedName)
	}

	provider := whopenai.New(types.ProviderConfig{
		APIKey:     apiKey,
		BaseURL:    normalizeBaseURL(selectedConfig.BaseURL),
		Headers:    selectedConfig.ExtraHeaders,
		MaxRetries: noWormholeRetries(),
	})

	return &OpenAIEmbedding{
		provider:   provider,
		model:      embedCfg.Model,
		dimensions: embedCfg.Dimensions,
		batchSize:  embedCfg.BatchSize,
	}, nil
}

// Embed generates embeddings for the given texts
func (e *OpenAIEmbedding) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	var allEmbeddings [][]float32

	// Process in batches
	for i := 0; i < len(texts); i += e.batchSize {
		end := i + e.batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		resp, err := e.provider.Embeddings(ctx, types.EmbeddingsRequest{
			Model: e.model,
			Input: batch,
		})

		if err != nil {
			return nil, fmt.Errorf("embedding request failed: %w", err)
		}

		for _, data := range resp.Embeddings {
			// Convert []float64 to []float32
			embedding := make([]float32, len(data.Embedding))
			for j, v := range data.Embedding {
				embedding[j] = float32(v)
			}
			allEmbeddings = append(allEmbeddings, embedding)
		}
	}

	return allEmbeddings, nil
}

// Dimensions returns the embedding dimension count
func (e *OpenAIEmbedding) Dimensions() int {
	return e.dimensions
}

// ModelName returns the embedding model name
func (e *OpenAIEmbedding) ModelName() string {
	return e.model
}

// Close is a no-op for OpenAI clients
func (e *OpenAIEmbedding) Close() {
	// OpenAI clients don't need explicit cleanup
}
