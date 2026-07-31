package cmd

import (
	"context"
	"fmt"
	"strings"

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
	Content   string
	Model     string
	TokensIn  int
	TokensOut int
	ID        string
}

func newPingOpenAIClientForConfig(name string, cfg providers.Config) (*pingOpenAIClient, error) {
	apiKey := providers.GetAPIKey(name, cfg)
	if apiKey == "" && cfg.GetRequiresKey() {
		return nil, fmt.Errorf("no API key for %s", name)
	}

	provider := whopenai.New(whtypes.ProviderConfig{
		APIKey:          apiKey,
		BaseURL:         normalizePingOpenAIBaseURL(cfg.BaseURL),
		Headers:         cfg.ExtraHeaders,
		MaxRetries:      noPingWormholeRetries(),
		UseResponsesAPI: isResponsesAPIProvider(cfg),
	})
	return &pingOpenAIClient{
		provider:         provider,
		model:            cfg.Model,
		extraBody:        cfg.ExtraBody,
		extraBodyByModel: cfg.ExtraBodyByModel,
	}, nil
}

func (c *pingOpenAIClient) Chat(ctx context.Context, systemPrompt, userContent string, maxTokens int) (*pingOpenAIResult, error) {
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

func (c *pingOpenAIClient) providerOptions() map[string]any {
	if len(c.extraBody) == 0 && len(c.extraBodyByModel[c.model]) == 0 {
		return nil
	}

	opts := make(map[string]any, len(c.extraBody)+len(c.extraBodyByModel[c.model]))
	for key, value := range c.extraBody {
		opts[key] = value
	}
	if perModel, ok := c.extraBodyByModel[c.model]; ok {
		for key, value := range perModel {
			opts[key] = value
		}
	}
	return opts
}

func noPingWormholeRetries() *int {
	noRetries := 0
	return &noRetries
}

func normalizePingOpenAIBaseURL(url string) string {
	if url == "" {
		return ""
	}
	url = strings.TrimSuffix(url, "/")
	if !strings.HasSuffix(url, "/v1") && !strings.HasSuffix(url, "/v4") {
		url += "/v1"
	}
	return url
}

func isResponsesAPIProvider(cfg providers.Config) bool {
	if cfg.ProviderType != "openai" {
		return false
	}
	return strings.Contains(strings.ToLower(cfg.BaseURL), "api.openai.com")
}
