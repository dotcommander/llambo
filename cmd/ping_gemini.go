package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	whgemini "github.com/garyblankenship/wormhole/v3/providers/gemini"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

type pingGeminiClient struct {
	provider whtypes.Provider
	model    string
}

type pingGeminiResult struct {
	Content    string
	Model      string
	TokensIn   int
	TokensOut  int
	TTFB       time.Duration
	Generation time.Duration
}

func newPingGeminiClient(apiKey, baseURL, model string) *pingGeminiClient {
	noRetry := 0
	provider := whgemini.New(apiKey, whtypes.ProviderConfig{
		APIKey:     apiKey,
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		MaxRetries: &noRetry,
	})
	return &pingGeminiClient{provider: provider, model: model}
}

func (c *pingGeminiClient) Chat(ctx context.Context, systemPrompt, userContent string, maxTokens int) (*pingGeminiResult, error) {
	req := c.textRequest(systemPrompt, userContent, maxTokens)

	resp, err := c.provider.Text(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("gemini API: %w", err)
	}

	result := &pingGeminiResult{
		Content: strings.TrimSpace(resp.Text),
		Model:   string(resp.Model),
	}
	if resp.Usage != nil {
		result.TokensIn = resp.Usage.PromptTokens
		result.TokensOut = resp.Usage.CompletionTokens
	}
	return result, nil
}

func (c *pingGeminiClient) ChatStream(ctx context.Context, systemPrompt, userContent string, maxTokens int) (*pingGeminiResult, error) {
	started := time.Now()
	stream, err := c.provider.Stream(ctx, c.textRequest(systemPrompt, userContent, maxTokens))
	if err != nil {
		return nil, err
	}
	stats, err := consumePingStream(stream, started)
	if err != nil {
		return nil, err
	}
	return &pingGeminiResult{
		Content:    stats.Content,
		Model:      c.model,
		TokensIn:   stats.TokensIn,
		TokensOut:  stats.TokensOut,
		TTFB:       stats.TTFB,
		Generation: stats.Generation,
	}, nil
}

func (c *pingGeminiClient) textRequest(systemPrompt, userContent string, maxTokens int) whtypes.TextRequest {
	req := whtypes.TextRequest{
		BaseRequest: whtypes.BaseRequest{
			Model: c.model,
		},
		SystemPrompt: systemPrompt,
		Messages: []whtypes.Message{
			whtypes.NewUserMessage(userContent),
		},
	}
	if maxTokens > 0 {
		req.MaxTokens = &maxTokens
	}
	return req
}
