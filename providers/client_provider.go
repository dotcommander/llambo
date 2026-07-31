package providers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	whconfig "github.com/garyblankenship/wormhole/v3/config"
	whproviders "github.com/garyblankenship/wormhole/v3/providers"
	whanthropic "github.com/garyblankenship/wormhole/v3/providers/anthropic"
	whgemini "github.com/garyblankenship/wormhole/v3/providers/gemini"
	whopenai "github.com/garyblankenship/wormhole/v3/providers/openai"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func noWormholeRetries() *int {
	noRetries := 0
	return &noRetries
}

func createProviderForConfigWithKey(name string, cfg Config, apiKey string) (whtypes.Provider, error) {
	slog.Debug("provider client construction: entry", "provider", name, "model", cfg.Model, "provider_type", cfg.GetProviderType())
	if apiKey == "" && cfg.GetRequiresKey() {
		slog.Debug("provider client construction: exit", "provider", name, "model", cfg.Model, "outcome", "error")
		return nil, fmt.Errorf("no API key for %s", name)
	}

	providerConfig := whtypes.ProviderConfig{
		APIKey:     apiKey,
		BaseURL:    providerConfigBaseURL(cfg),
		Headers:    cfg.ExtraHeaders,
		MaxRetries: noWormholeRetries(),
	}

	switch cfg.GetProviderType() {
	case "anthropic":
		client := whanthropic.New(providerConfig)
		client.BaseProvider = newBaseProvider("anthropic", providerConfig)
		slog.Debug("provider client construction: exit", "provider", name, "model", cfg.Model, "outcome", "success")
		return client, nil
	case "gemini":
		client := whgemini.New(apiKey, providerConfig)
		slog.Debug("provider client construction: exit", "provider", name, "model", cfg.Model, "outcome", "success")
		return client, nil
	default:
		client := whopenai.New(providerConfig)
		client.BaseProvider = newBaseProvider("openai", providerConfig)
		slog.Debug("provider client construction: exit", "provider", name, "model", cfg.Model, "outcome", "success")
		return client, nil
	}
}

func newBaseProvider(providerType string, cfg whtypes.ProviderConfig) *whproviders.BaseProvider {
	var authStrategy whproviders.AuthStrategy
	switch providerType {
	case "anthropic":
		authStrategy = (&whproviders.AuthStrategyFactory{}).CreateAuthStrategy("anthropic", cfg)
	case "gemini":
		authStrategy = &whproviders.NoAuthStrategy{}
	}
	return whproviders.NewBaseProviderWithAuth(providerType, cfg, nil, authStrategy, &http.Client{Timeout: providerHTTPTimeout(cfg)})
}

func providerConfigBaseURL(cfg Config) string {
	switch cfg.GetProviderType() {
	case "anthropic":
		if cfg.BaseURL == "" {
			return defaultAnthropicBaseURL
		}
		return strings.TrimSuffix(cfg.BaseURL, "/")
	case "gemini":
		if cfg.BaseURL == "" {
			return defaultGeminiBaseURL
		}
		return strings.TrimSuffix(cfg.BaseURL, "/")
	default:
		if cfg.BaseURL == "" {
			return defaultOpenAIBaseURL
		}
		return normalizeBaseURL(cfg.BaseURL)
	}
}

func providerHTTPTimeout(cfg whtypes.ProviderConfig) time.Duration {
	if cfg.Timeout > 0 {
		return time.Duration(cfg.Timeout) * time.Second
	}
	if cfg.Timeout == 0 {
		return 0
	}
	return whconfig.GetDefaultHTTPTimeout()
}

// normalizeBaseURL ensures OpenAI-compatible wormhole providers receive a base URL
// that already includes the API version before they append /chat/completions.
func normalizeBaseURL(url string) string {
	if url == "" {
		return "" // default OpenAI URL
	}

	// Remove trailing slash
	url = strings.TrimSuffix(url, "/")

	// Ensure /v1 suffix - SDK appends /chat/completions directly
	if !strings.HasSuffix(url, "/v1") && !strings.HasSuffix(url, "/v4") {
		url = url + "/v1"
	}

	return url
}

// MaxTokensFromConfigs returns the maximum token limit across all configs
func MaxTokensFromConfigs(configs map[string]Config, defaultMax int) int {
	maxTokens := defaultMax
	for _, cfg := range configs {
		if cfg.MaxTokens > maxTokens {
			maxTokens = cfg.MaxTokens
		}
	}
	return maxTokens
}
