package writingcampaign

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	Now        func() time.Time
}

type chatRequest struct {
	Model     string           `json:"model"`
	Messages  []chatMessage    `json:"messages"`
	MaxTokens int              `json:"max_tokens"`
	Provider  *providerRouting `json:"provider,omitempty"`
}

type providerRouting struct {
	MaxPrice providerMaxPrice `json:"max_price"`
}

type providerMaxPrice struct {
	Prompt     float64 `json:"prompt"`
	Completion float64 `json:"completion"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *Client) Execute(ctx context.Context, model Model, systemPrompt, source string, maxTokens, attempt int) Attempt {
	started := c.now()
	result := Attempt{Attempt: attempt, MaxCompletionTokens: maxTokens, StartedAt: started, RequestedModel: model.OpenRouterModelID}
	requestBody := chatRequest{Model: model.OpenRouterModelID, Messages: []chatMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: source}}, MaxTokens: maxTokens}
	if model.DynamicRouting {
		requestBody.Provider = &providerRouting{MaxPrice: providerMaxPrice{Prompt: model.RoutingMaxInputPer1M, Completion: model.RoutingMaxOutputPer1M}}
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		result.Error = err.Error()
		return c.complete(result, started)
	}
	endpoint, err := chatEndpoint(c.BaseURL)
	if err != nil {
		result.Error = err.Error()
		return c.complete(result, started)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		result.Error = err.Error()
		return c.complete(result, started)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range c.Headers {
		if !sensitiveHeader(key) {
			req.Header.Set(key, value)
		}
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		result.Error = err.Error()
		return c.complete(result, started)
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode
	result.ResponseHeaders = safeResponseHeaders(resp.Header)
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if len(raw) > 0 && json.Valid(raw) {
		result.RawResponse = append(json.RawMessage(nil), raw...)
	}
	if readErr != nil {
		result.Error = fmt.Sprintf("read response: %v", readErr)
		return c.complete(result, started)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("OpenRouter HTTP %d: %s", resp.StatusCode, compactError(raw))
		return c.complete(result, started)
	}
	var decoded chatResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		result.Error = fmt.Sprintf("parse response: %v", err)
		return c.complete(result, started)
	}
	result = applyChatResponse(result, model, decoded)
	return c.complete(result, started)
}

func (c *Client) complete(result Attempt, started time.Time) Attempt {
	result.CompletedAt = c.now()
	result.LatencyMS = result.CompletedAt.Sub(started).Milliseconds()
	if result.LatencyMS > 0 && result.Usage.CompletionTokens > 0 {
		result.Usage.TokensPerSecond = float64(result.Usage.CompletionTokens) / (float64(result.LatencyMS) / 1000)
	}
	return result
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func chatEndpoint(base string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid OpenRouter base URL %q", base)
	}
	if strings.HasSuffix(parsed.Path, "/v1") {
		parsed.Path += "/chat/completions"
	} else {
		parsed.Path += "/v1/chat/completions"
	}
	return parsed.String(), nil
}

func safeResponseHeaders(headers http.Header) map[string][]string {
	out := make(map[string][]string)
	for key, values := range headers {
		if sensitiveHeader(key) {
			continue
		}
		out[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	return out
}

func sensitiveHeader(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return key == "authorization" || key == "proxy-authorization" || key == "cookie" || key == "set-cookie" || strings.Contains(key, "api-key")
}

func compactError(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 500 {
		text = text[:500] + "…"
	}
	return text
}
