package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestWormholeChat_ProviderOptionsReachWire(t *testing.T) {
	t.Parallel()

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","created":1,"model":"gpt-5-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`))
	}))
	t.Cleanup(srv.Close)

	provider, err := NewOpenAI(map[string]Config{
		"openai": {
			BaseURL: srv.URL,
			Model:   "gpt-5-mini",
			Enabled: true,
			APIKey:  "key",
			ExtraBody: map[string]any{
				"reasoning": map[string]any{"effort": "low"},
				"provider": map[string]any{
					"sort":               "throughput",
					"allow_fallbacks":    false,
					"require_parameters": true,
					"data_collection":    "deny",
				},
				"top_k": 10,
			},
			ExtraBodyByModel: map[string]map[string]any{
				"gpt-5-mini": {"reasoning": map[string]any{"effort": "minimal"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	t.Cleanup(provider.Shutdown)

	ctx := WithRequestMetadata(context.Background(), map[string]string{"trace_id": "abc123"})
	ctx = WithJSONOverrides(ctx, map[string]any{"top_k": 50})
	result, err := provider.ChatWithInfoContext(ctx, "system", "user")
	if err != nil {
		t.Fatalf("ChatWithInfoContext: %v", err)
	}
	if result.Content != "ok" || result.Usage == nil || result.Usage.TotalTokens != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}

	if got["max_completion_tokens"] != nil {
		t.Fatalf("did not expect max tokens without config, got %+v", got["max_completion_tokens"])
	}
	if got["top_k"] != float64(50) {
		t.Fatalf("expected context top_k override 50, got %+v", got["top_k"])
	}
	reasoning, ok := got["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "minimal" {
		t.Fatalf("expected per-model reasoning effort, got %+v", got["reasoning"])
	}
	openRouterProvider, ok := got["provider"].(map[string]any)
	if !ok ||
		openRouterProvider["sort"] != "throughput" ||
		openRouterProvider["allow_fallbacks"] != false ||
		openRouterProvider["require_parameters"] != true ||
		openRouterProvider["data_collection"] != "deny" {
		t.Fatalf("expected OpenRouter provider controls on wire, got %+v", got["provider"])
	}
	metadata, ok := got["metadata"].(map[string]any)
	if !ok || metadata["trace_id"] != "abc123" {
		t.Fatalf("expected metadata on wire, got %+v", got["metadata"])
	}
}

func TestWormholeChat_RateLimitRotatesKeyOnce(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		count := len(auths)
		mu.Unlock()

		if count == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","created":1,"model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	t.Cleanup(srv.Close)

	provider, err := NewOpenAI(map[string]Config{
		"openai": {
			BaseURL: srv.URL,
			Model:   "gpt-test",
			Enabled: true,
			APIKeys: []string{"key1", "key2"},
		},
	})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	t.Cleanup(provider.Shutdown)

	start := time.Now()
	result, err := provider.ChatWithInfoContext(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("ChatWithInfoContext: %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("expected content ok, got %q", result.Content)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("429 rotation took too long, wormhole retry may not be disabled")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(auths) != 2 {
		t.Fatalf("expected exactly 2 requests, got %d auths=%v", len(auths), auths)
	}
	if auths[0] != "Bearer key1" || auths[1] != "Bearer key2" {
		t.Fatalf("expected one llambo rotation from key1 to key2, got %v", auths)
	}
	if got := provider.oc.KeyRotator.GetKey("openai"); got != "key2" {
		t.Fatalf("expected active key key2, got %q", got)
	}
}

func TestWormholeChat_StreamRateLimitRotatesBeforeFirstChunk(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		count := len(auths)
		mu.Unlock()

		if count == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	provider, err := NewOpenAI(map[string]Config{
		"openai": {
			BaseURL: srv.URL,
			Model:   "gpt-test",
			Enabled: true,
			APIKeys: []string{"key1", "key2"},
		},
	})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	t.Cleanup(provider.Shutdown)

	var chunks []ChatStreamChunk
	result, err := provider.ChatStreamWithInfoContext(context.Background(), "system", "user", func(chunk ChatStreamChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStreamWithInfoContext: %v", err)
	}
	if result.Content != "hello" || result.Usage == nil || result.Usage.TotalTokens != 5 || result.FinishReason != "stop" {
		t.Fatalf("unexpected stream result: %+v", result)
	}
	if len(chunks) == 0 || chunks[0].ContentDelta != "hello" {
		t.Fatalf("expected streamed hello chunk, got %+v", chunks)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(auths) != 2 {
		t.Fatalf("expected exactly 2 stream requests, got %d auths=%v", len(auths), auths)
	}
	if auths[0] != "Bearer key1" || auths[1] != "Bearer key2" {
		t.Fatalf("expected stream rotation from key1 to key2, got %v", auths)
	}
}

func TestWormholeChat_RateLimitTripsCircuitWhenNoRotationAvailable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}))
	t.Cleanup(srv.Close)

	provider, err := NewOpenAI(map[string]Config{
		"openai": {
			BaseURL: srv.URL,
			Model:   "gpt-test",
			Enabled: true,
			APIKey:  "key1",
		},
	})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	t.Cleanup(provider.Shutdown)

	_, err = provider.ChatWithInfoContext(context.Background(), "system", "user")
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if provider.oc.CircuitBreaker.IsHealthy("openai") {
		t.Fatal("expected circuit breaker to mark provider unhealthy after rate limit")
	}
}
