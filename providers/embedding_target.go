package providers

import (
	"context"
	"fmt"
	whopenai "github.com/garyblankenship/wormhole/v3/providers/openai"
	"github.com/garyblankenship/wormhole/v3/types"
	"sort"
)

type EmbeddingResult struct {
	Vectors  [][]float32
	Provider string
	Model    string
}

func (e *OpenAIEmbedding) EmbedResolved(ctx context.Context, texts []string, target ResolvedTarget) (EmbeddingResult, error) {
	if err := e.ValidateEmbeddingTarget(target); err != nil {
		return EmbeddingResult{}, err
	}
	if target.Model() == "" {
		vectors, err := e.Embed(ctx, texts)
		return EmbeddingResult{Vectors: vectors, Provider: e.providerName, Model: e.model}, err
	}
	var names []string
	for name, cfg := range e.configs {
		if cfg.Enabled && target.Allows(name) {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := embeddingPriority(e.configs[names[i]]), embeddingPriority(e.configs[names[j]])
		if a == b {
			return names[i] < names[j]
		}
		return a < b
	})
	for i, name := range names {
		if name == e.providerName {
			names[0], names[i] = names[i], names[0]
			break
		}
	}
	if len(names) == 0 {
		return EmbeddingResult{}, fmt.Errorf("no eligible embedding provider")
	}
	name := names[0]
	cfg := e.configs[name]
	switch cfg.GetProviderType() {
	case "anthropic", "gemini":
		return EmbeddingResult{}, fmt.Errorf("provider %s protocol does not support embeddings", name)
	}
	key := GetAPIKey(name, cfg)
	if key == "" && cfg.GetRequiresKey() {
		return EmbeddingResult{}, fmt.Errorf("embedding client init for %s: no API key", name)
	}
	providerConfig := types.ProviderConfig{APIKey: key, BaseURL: NormalizeOpenAIBaseURL(cfg.BaseURL), Headers: cfg.ExtraHeaders, MaxRetries: noWormholeRetries()}
	if key == "" {
		// Keyless endpoints (requires_key=false): Bearer refuses empty keys
		// pre-flight, so select the local-endpoint no-auth strategy.
		providerConfig = providerConfig.WithNoAuth()
	}
	provider := whopenai.New(providerConfig)
	selected := &OpenAIEmbedding{provider: provider, model: target.Model(), dimensions: e.dimensions, batchSize: e.batchSize}
	vectors, err := selected.Embed(ctx, texts)
	return EmbeddingResult{Vectors: vectors, Provider: name, Model: target.Model()}, err
}

func embeddingPriority(cfg Config) int {
	if cfg.Priority <= 0 {
		return 999
	}
	return cfg.Priority
}

func (e *OpenAIEmbedding) ValidateEmbeddingTarget(target ResolvedTarget) error {
	if target.Model() == "" {
		switch e.configs[e.providerName].GetProviderType() {
		case "anthropic", "gemini":
			return fmt.Errorf("provider %s protocol does not support embeddings", e.providerName)
		}
		return nil
	}
	names := make([]string, 0)
	for name, cfg := range e.configs {
		if cfg.Enabled && target.Allows(name) {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := embeddingPriority(e.configs[names[i]]), embeddingPriority(e.configs[names[j]])
		if a == b {
			return names[i] < names[j]
		}
		return a < b
	})
	for i, name := range names {
		if name == e.providerName {
			names[0], names[i] = names[i], names[0]
			break
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("no eligible embedding provider")
	}
	switch e.configs[names[0]].GetProviderType() {
	case "anthropic", "gemini":
		return fmt.Errorf("provider %s protocol does not support embeddings", names[0])
	}
	return nil
}
