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

// Repeated string literals consolidated by goconst.
const (
	anthropic1 = "anthropic"
	gemini1    = "gemini"
)

// chatLog returns a scoped logger bound to the current default handler, so
// tests that replace slog.SetDefault stay observable. Package-level slog
// calls are banned in functions that hold a request context.
func chatLog() *slog.Logger { return slog.New(slog.Default().Handler()) }

func noWormholeRetries() *int {
	noRetries := 0
	return &noRetries
}

func createProviderForConfigWithKey(name string, cfg Config, apiKey string) (whtypes.Provider, error) {
	chatLog().Debug("provider client construction: entry", "provider", name, "model", cfg.Model, "provider_type", cfg.GetProviderType())
	if apiKey == "" && cfg.GetRequiresKey() {
		chatLog().Debug("provider client construction: exit", "provider", name, "model", cfg.Model, "outcome", "error")
		return nil, fmt.Errorf("no API key for %s", name)
	}

	providerConfig := whtypes.ProviderConfig{
		APIKey:     apiKey,
		BaseURL:    providerConfigBaseURL(cfg),
		Headers:    cfg.ExtraHeaders,
		MaxRetries: noWormholeRetries(),
	}
	if apiKey == "" {
		// Keyless endpoints (requires_key=false, e.g. local LM Studio):
		// wormhole's Bearer strategy refuses empty keys before sending, so
		// select the no-auth strategy intended for local OpenAI-compatible
		// servers instead of failing client-side with AUTH_ERROR.
		providerConfig = providerConfig.WithNoAuth()
	}

	switch cfg.GetProviderType() {
	case anthropic1:
		client := whanthropic.New(providerConfig)
		client.BaseProvider = newBaseProvider(anthropic1, providerConfig)
		chatLog().Debug("provider client construction: exit", "provider", name, "model", cfg.Model, "outcome", "success")
		return client, nil
	case gemini1:
		client := whgemini.New(apiKey, providerConfig)
		chatLog().Debug("provider client construction: exit", "provider", name, "model", cfg.Model, "outcome", "success")
		return client, nil
	default:
		client := whopenai.New(providerConfig)
		client.BaseProvider = newBaseProvider(openai1, providerConfig)
		chatLog().Debug("provider client construction: exit", "provider", name, "model", cfg.Model, "outcome", "success")
		return client, nil
	}
}

func newBaseProvider(providerType string, cfg whtypes.ProviderConfig) *whproviders.BaseProvider {
	var authStrategy whproviders.AuthStrategy
	switch providerType {
	case anthropic1:
		authStrategy = (&whproviders.AuthStrategyFactory{}).CreateAuthStrategy(anthropic1, cfg)
	case gemini1:
		authStrategy = &whproviders.NoAuthStrategy{}
	}
	return whproviders.NewBaseProviderWithAuth(providerType, cfg, nil, authStrategy, &http.Client{Timeout: providerHTTPTimeout(cfg)})
}

func providerConfigBaseURL(cfg Config) string {
	switch cfg.GetProviderType() {
	case anthropic1:
		if cfg.BaseURL == "" {
			return defaultAnthropicBaseURL
		}
		return strings.TrimSuffix(cfg.BaseURL, "/")
	case gemini1:
		if cfg.BaseURL == "" {
			return defaultGeminiBaseURL
		}
		return strings.TrimSuffix(cfg.BaseURL, "/")
	default:
		if cfg.BaseURL == "" {
			return defaultOpenAIBaseURL
		}
		return NormalizeOpenAIBaseURL(cfg.BaseURL)
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

// NormalizeOpenAIBaseURL ensures OpenAI-compatible consumers receive a base URL
// that already includes the API version before they append endpoint paths.
func NormalizeOpenAIBaseURL(url string) string {
	if url == "" {
		return "" // default OpenAI URL
	}

	// Remove trailing slash
	url = strings.TrimSuffix(url, "/")

	// Ensure /v1 suffix - SDK appends /chat/completions directly
	if !strings.HasSuffix(url, "/v1") && !strings.HasSuffix(url, "/v4") {
		url += "/v1"
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
