package cmd

import (
	"context"
	"fmt"
	"strings"

	whgemini "github.com/garyblankenship/wormhole/v3/providers/gemini"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

type pingGeminiClient struct {
	provider whtypes.Provider
	model    string
}

type pingGeminiResult struct {
	Content   string
	Model     string
	TokensIn  int
	TokensOut int
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
