package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dotcommander/llambo/providers"
	whopenai "github.com/garyblankenship/wormhole/v3/providers/openai"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

type pingOpenAIClient struct {
	provider         whtypes.Provider
	model            string
	extraBody        map[string]any
	extraBodyByModel map[string]map[string]any
}

type pingOpenAIResult struct {
	Content    string
	Model      string
	TokensIn   int
	TokensOut  int
	ID         string
	TTFB       time.Duration
	Generation time.Duration
}

func newPingOpenAIClientForConfig(name string, cfg providers.Config) (*pingOpenAIClient, error) {
	apiKey := providers.GetAPIKey(name, cfg)
	if apiKey == "" && cfg.GetRequiresKey() {
		return nil, fmt.Errorf("no API key for %s", name)
	}

	providerConfig := whtypes.ProviderConfig{
		APIKey:          apiKey,
		BaseURL:         providers.NormalizeOpenAIBaseURL(cfg.BaseURL),
		Headers:         cfg.ExtraHeaders,
		MaxRetries:      noPingWormholeRetries(),
		UseResponsesAPI: isResponsesAPIProvider(cfg),
	}
	if apiKey == "" {
		// Keyless endpoints (requires_key=false, e.g. local LM Studio):
		// wormhole's Bearer strategy refuses empty keys before sending; use
		// the no-auth strategy intended for local OpenAI-compatible servers.
		providerConfig = providerConfig.WithNoAuth()
	}
	provider := whopenai.New(providerConfig)
	return &pingOpenAIClient{
		provider:         provider,
		model:            cfg.Model,
		extraBody:        cfg.ExtraBody,
		extraBodyByModel: cfg.ExtraBodyByModel,
	}, nil
}

func (c *pingOpenAIClient) Chat(ctx context.Context, systemPrompt, userContent string, maxTokens int) (*pingOpenAIResult, error) {
	req := c.textRequest(systemPrompt, userContent, maxTokens)

	resp, err := c.provider.Text(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai API: %w", err)
	}

	result := &pingOpenAIResult{
		ID:      resp.ID,
		Model:   resp.Model,
		Content: strings.TrimSpace(resp.Text),
	}
	if resp.Usage != nil {
		result.TokensIn = resp.Usage.PromptTokens
		result.TokensOut = resp.Usage.CompletionTokens
	}
	return result, nil
}

func (c *pingOpenAIClient) ChatStream(ctx context.Context, systemPrompt, userContent string, maxTokens int) (*pingOpenAIResult, error) {
	started := time.Now()
	stream, err := c.provider.Stream(ctx, c.textRequest(systemPrompt, userContent, maxTokens))
	if err != nil {
		return nil, err
	}
	stats, err := consumePingStream(stream, started)
	if err != nil {
		return nil, err
	}
	return &pingOpenAIResult{
		Content:    stats.Content,
		Model:      c.model,
		TokensIn:   stats.TokensIn,
		TokensOut:  stats.TokensOut,
		TTFB:       stats.TTFB,
		Generation: stats.Generation,
	}, nil
}

func (c *pingOpenAIClient) textRequest(systemPrompt, userContent string, maxTokens int) whtypes.TextRequest {
	messages := make([]whtypes.Message, 0, 2)
	if systemPrompt != "" {
		messages = append(messages, whtypes.NewSystemMessage(systemPrompt))
	}
	messages = append(messages, whtypes.NewUserMessage(userContent))

	req := whtypes.TextRequest{
		BaseRequest: whtypes.BaseRequest{
			Model:           c.model,
			ProviderOptions: c.providerOptions(),
		},
		Messages:     messages,
		SystemPrompt: systemPrompt,
	}
	if maxTokens > 0 {
		req.MaxTokens = &maxTokens
	}
	return req
}

func (c *pingOpenAIClient) providerOptions() map[string]any {
	return providers.ExtraBodyForModel(providers.Config{
		Model:            c.model,
		ExtraBody:        c.extraBody,
		ExtraBodyByModel: c.extraBodyByModel,
	})
}

func noPingWormholeRetries() *int {
	noRetries := 0
	return &noRetries
}

func isResponsesAPIProvider(cfg providers.Config) bool {
	if cfg.ProviderType != "openai" {
		return false
	}
	return strings.Contains(strings.ToLower(cfg.BaseURL), "api.openai.com")
}
