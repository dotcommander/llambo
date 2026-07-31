package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
)

func newWormholeHandlerTestServer(t *testing.T, configs map[string]providers.Config) *Server {
	t.Helper()

	dir := t.TempDir()
	routing := providers.RoutingConfig{
		MetricsPath: filepath.Join(dir, "routing-metrics.json"),
		EventsPath:  filepath.Join(dir, "routing-events.jsonl"),
	}
	provider, err := providers.NewOpenAIWithRoutingCallbacks(configs, routing, nil, nil)
	if err != nil {
		t.Fatalf("NewOpenAIWithRoutingCallbacks: %v", err)
	}
	queue := providers.NewBackendQueue(provider.GetOpenAIClients(), configs)
	server := &Server{
		provider:   provider,
		queue:      queue,
		jobManager: NewJobManager(context.Background(), queue),
		configs:    configs,
		startTime:  time.Now(),
	}
	t.Cleanup(func() {
		server.jobManager.StopCleanup()
		provider.Shutdown()
	})
	return server
}

func TestWormholeGateway_ChatCompletionShapeHeadersAndCost(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","created":1,"model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`))
	}))
	t.Cleanup(upstream.Close)

	configs := map[string]providers.Config{
		"openai": {
			BaseURL:      upstream.URL,
			Model:        "gpt-test",
			Enabled:      true,
			APIKey:       "key",
			InputCostPM:  1,
			OutputCostPM: 2,
		},
	}
	server := newWormholeHandlerTestServer(t, configs)

	req := ChatCompletionRequest{
		Model: "gpt-test",
		Messages: []Message{
			{Role: "system", Content: "system"},
			{Role: "user", Content: "user"},
		},
	}
	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-Llambo-Provider"); got != "openai" {
		t.Fatalf("expected X-Llambo-Provider openai, got %q", got)
	}
	if got := rr.Header().Get("X-Llambo-Model"); got != "gpt-test" {
		t.Fatalf("expected X-Llambo-Model gpt-test, got %q", got)
	}

	resp := decodeResponse[ChatCompletionResponse](t, rr)
	if resp.Object != "chat.completion" || resp.Model != "gpt-test" || len(resp.Choices) != 1 {
		t.Fatalf("unexpected response shape: %+v", resp)
	}
	if resp.Choices[0].Message == nil || resp.Choices[0].Message.Role != "assistant" || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected message choice: %+v", resp.Choices[0])
	}
	if resp.XLlambo == nil || resp.XLlambo.Backend != "openai" || resp.XLlambo.TotalTokens != 7 || resp.XLlambo.CostUSD <= 0 {
		t.Fatalf("unexpected x_llambo metadata: %+v", resp.XLlambo)
	}

	stats := server.provider.GetOpenAIClients().CostTracker.GetStats()["openai"]
	if stats.Requests != 1 || stats.PromptTokens != 3 || stats.CompletionTokens != 4 || stats.TotalTokens != 7 || stats.TotalCostUSD <= 0 {
		t.Fatalf("unexpected cost tracker stats: %+v", stats)
	}
}

func TestWormholeGateway_AnthropicMessagesShapeUsesSameProviderPath(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","created":1,"model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"anthropic ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":6,"total_tokens":11}}`))
	}))
	t.Cleanup(upstream.Close)

	server := newWormholeHandlerTestServer(t, map[string]providers.Config{
		"openai": {BaseURL: upstream.URL, Model: "gpt-test", Enabled: true, APIKey: "key"},
	})

	req := map[string]any{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-Llambo-Provider"); got != "openai" {
		t.Fatalf("expected X-Llambo-Provider openai, got %q", got)
	}

	var resp AnthropicMessagesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode anthropic response: %v", err)
	}
	if resp.Type != "message" || resp.Role != "assistant" || resp.Model != "gpt-test" {
		t.Fatalf("unexpected anthropic response shape: %+v", resp)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "anthropic ok" {
		t.Fatalf("unexpected anthropic content: %+v", resp.Content)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 6 {
		t.Fatalf("unexpected anthropic usage: %+v", resp.Usage)
	}
}

func TestWormholeGateway_StreamUsageCarriesXLlambo(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"stream ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":9,\"total_tokens\":17}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(upstream.Close)

	server := newWormholeHandlerTestServer(t, map[string]providers.Config{
		"openai": {BaseURL: upstream.URL, Model: "gpt-test", Enabled: true, APIKey: "key"},
	})

	req := ChatCompletionRequest{
		Model:  "gpt-test",
		Stream: true,
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
	}
	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", got)
	}

	body := rr.Body.String()
	if !strings.Contains(body, `"content":"stream ok"`) {
		t.Fatalf("expected stream content delta, body=%s", body)
	}
	if !strings.Contains(body, `"usage":{"prompt_tokens":8,"completion_tokens":9,"total_tokens":17}`) {
		t.Fatalf("expected final usage chunk, body=%s", body)
	}
	if !strings.Contains(body, `"x_llambo":{"backend":"openai","model":"gpt-test"`) {
		t.Fatalf("expected x_llambo stream metadata, body=%s", body)
	}
}
