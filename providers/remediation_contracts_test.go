package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestExactTargetPreservesCaseAndEmbeddedSlash(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{"router": {Enabled: true, Model: "default", Models: []string{"Org/Case"}}, "other": {Enabled: true, Model: "Other"}}
	target, err := ResolveTarget(configs, "router/Org/Case", "")
	if err != nil || target.Model() != "Org/Case" || !target.Allows("router") || target.Allows("other") {
		t.Fatalf("target %+v: %v", target, err)
	}
	if _, err := ResolveTarget(configs, "org/case", ""); err == nil {
		t.Fatal("case mismatch admitted")
	}
	bare, err := ResolveTarget(configs, "Org/Case", "")
	if err != nil || !bare.Allows("router") {
		t.Fatalf("embedded slash lost: %v", err)
	}
}
func TestStructuredHistoryAndExactQueueModelReachWormhole(t *testing.T) {
	t.Parallel()
	requests := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value map[string]any
		_ = json.NewDecoder(r.Body).Decode(&value)
		requests <- value
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "test", "model": "Exact/Case", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}}})
	}))
	t.Cleanup(server.Close)
	configs := map[string]Config{"fixture": {BaseURL: server.URL, Model: "default", Models: []string{"Exact/Case"}, Enabled: true, APIKey: "fixture"}}
	directory := t.TempDir()
	provider, err := NewOpenAIWithRoutingCallbacks(configs, RoutingConfig{MetricsPath: filepath.Join(directory, "metrics.json"), EventsPath: filepath.Join(directory, "events.jsonl")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.Shutdown)
	target, err := ResolveTarget(configs, "fixture/Exact/Case", "")
	if err != nil {
		t.Fatal(err)
	}
	request := StructuredChatRequest{Messages: []whtypes.Message{whtypes.NewSystemMessage("policy"), whtypes.NewUserMessage("first"), &whtypes.AssistantMessage{Content: "answer"}, whtypes.NewUserMessage("second")}, Stop: []string{"one", "two"}}
	if _, err := provider.ChatStructuredWithInfoContext(context.Background(), request, target); err != nil {
		t.Fatal(err)
	}
	got := <-requests
	if got["model"] != "Exact/Case" || len(got["messages"].([]any)) != 4 {
		t.Fatalf("wire %+v", got)
	}
	queue := NewBackendQueue(provider.GetOpenAIClients(), configs)
	job, err := queue.PrepareJob(Job{ID: "test", Request: &request, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	results := queue.Process(context.Background(), []Job{job})
	if len(results) != 1 || results[0].Error != nil {
		t.Fatalf("results %+v", results)
	}
	if got := <-requests; got["model"] != "Exact/Case" {
		t.Fatalf("queue replaced exact model: %+v", got)
	}
}
func TestKeyRotationVisitsEveryDistinctKey(t *testing.T) {
	t.Parallel()
	calls := 0
	result, err := executeChatAttemptWithKeyRotation(context.Background(), "fixture", Config{}, "", "", func(string, chatRequestResult) (bool, error) { return true, nil }, func(context.Context, string, Config, string, string) (chatRequestResult, error) {
		calls++
		if calls == 3 {
			return chatRequestResult{content: "ok", clientKey: "third"}, nil
		}
		return chatRequestResult{clientKey: fmt.Sprint(calls)}, NewOpenAIError("quota", 429, nil)
	})
	if err != nil || calls != 3 || result.content != "ok" {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
}
func TestConsumerFailureAndToolIndices(t *testing.T) {
	t.Parallel()
	source := errors.New("writer failed")
	stream := make(chan whtypes.TextChunk, 3)
	stream <- whtypes.TextChunk{ToolCalls: []whtypes.ToolCall{{Index: 1, ID: "call", Function: &whtypes.ToolCallFunction{Name: "fn", Arguments: "{"}}}}
	stream <- whtypes.TextChunk{ToolCalls: []whtypes.ToolCall{{Index: 1, Function: &whtypes.ToolCallFunction{Arguments: "}"}}}}
	close(stream)
	_, _, _, calls, _, _, err := consumeTextStream(stream, "model", func(c ChatStreamChunk) error {
		if c.ToolDeltas[0].Index != 1 {
			t.Fatal("index lost")
		}
		return nil
	})
	if err != nil || len(calls) != 1 || calls[0].Arguments != "{}" {
		t.Fatalf("calls=%+v err=%v", calls, err)
	}
	stream2 := make(chan whtypes.TextChunk, 1)
	stream2 <- whtypes.TextChunk{Text: "text"}
	close(stream2)
	_, _, _, _, _, _, err = consumeTextStream(stream2, "model", func(ChatStreamChunk) error { return source })
	if !IsConsumerError(err) || !errors.Is(err, source) {
		t.Fatalf("cause lost: %v", err)
	}
	breaker := NewCircuitBreaker([]string{"fixture"}, nil)
	breaker.RecordFailure("fixture", err)
	if breaker.GetHealth("fixture").Failures != 0 {
		t.Fatal("consumer penalized")
	}
}
func TestGatewayNormalizationAndExplicitZero(t *testing.T) {
	t.Parallel()
	cfg, err := (GatewayConfig{}).Normalize()
	if err != nil || cfg.MaxRetainedJobs != 1000 || cfg.ShutdownTimeoutSeconds != 95 {
		t.Fatalf("config=%+v err=%v", cfg, err)
	}
	if _, err := (GatewayConfig{HandlerTimeoutSeconds: 100, WriteTimeoutSeconds: 104, ShutdownTimeoutSeconds: 105}).Normalize(); err == nil {
		t.Fatal("invalid timeout admitted")
	}
	encoded, _ := json.Marshal(map[string]any{"max_retained_jobs": 0})
	if err := json.Unmarshal(encoded, &cfg); err == nil {
		t.Fatal("explicit zero admitted")
	}
	if ClassifyError(errors.New("model revision 429 available")) != UnknownError {
		t.Fatal("model number classified as HTTP status")
	}
}

func TestEmbeddingResolvedPinsEveryBatch(t *testing.T) {
	t.Parallel()
	models := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		models <- req.Model
		rows := make([]any, len(req.Input))
		for i := range rows {
			rows[i] = map[string]any{"index": i, "embedding": []float64{1, 2}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "model": req.Model, "data": rows, "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
	}))
	t.Cleanup(server.Close)
	configs := map[string]Config{"fixture": {BaseURL: server.URL, Enabled: true, APIKey: "fixture", Model: "chat", Models: []string{"Embed/Exact"}}}
	embedder, err := NewOpenAIEmbeddingWithConfig(EmbedConfig{Model: "default", BatchSize: 1}, configs)
	if err != nil {
		t.Fatal(err)
	}
	target, err := ResolveTarget(configs, "fixture/Embed/Exact", "default")
	if err != nil {
		t.Fatal(err)
	}
	result, err := embedder.EmbedResolved(context.Background(), []string{"a", "b", "c"}, target)
	if err != nil || len(result.Vectors) != 3 || result.Provider != "fixture" || result.Model != "Embed/Exact" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for range 3 {
		if model := <-models; model != "Embed/Exact" {
			t.Fatalf("batch used %q", model)
		}
	}
	unsupported := &OpenAIEmbedding{configs: map[string]Config{"anthropic": {Enabled: true, ProviderType: "anthropic"}}, providerName: "anthropic"}
	rejected := ResolvedTarget{model: "embed", providers: map[string]bool{"anthropic": true}}
	if unsupported.ValidateEmbeddingTarget(rejected) == nil {
		t.Fatal("unsupported protocol admitted")
	}
}

func TestStructuredAttemptsDoNotShareMutableOptions(t *testing.T) {
	t.Parallel()
	tool := *whtypes.NewTool("fn", "description", map[string]any{"properties": map[string]any{"x": map[string]any{"type": "string"}}})
	request := StructuredChatRequest{Messages: []whtypes.Message{whtypes.NewUserMessage("original")}, Tools: []whtypes.Tool{tool}, ResponseFormat: map[string]any{"type": "json_object"}, Stop: []string{"END"}}
	ctx := structuredContext(context.Background(), request)
	first := buildTextRequest(ctx, Config{Model: "model"}, "", "")
	first.Messages[0].(*whtypes.UserMessage).Content = "changed"
	first.Tools[0].InputSchema["properties"].(map[string]any)["x"] = "changed"
	first.Stop[0] = "changed"
	second := buildTextRequest(ctx, Config{Model: "model"}, "", "")
	if second.Messages[0].GetContent() != "original" || second.Stop[0] != "END" {
		t.Fatal("attempt mutated request")
	}
	if _, ok := second.Tools[0].InputSchema["properties"].(map[string]any)["x"].(map[string]any); !ok {
		t.Fatal("tool schema shared")
	}
}

func TestExactTargetConstrainsCanaryAndEveryFallback(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{"first": {Enabled: true, Model: "default", Models: []string{"Exact"}}, "second": {Enabled: true, Model: "Exact"}, "forbidden": {Enabled: true, Model: "Other"}}
	var counter uint64
	oc := &OpenAIClients{Backends: []Backend{{Name: "first"}, {Name: "second"}, {Name: "forbidden"}}, CircuitBreaker: NewCircuitBreaker([]string{"first", "second", "forbidden"}, nil), RoutingConfig: RoutingConfig{Canary: &CanaryConfig{Provider: "forbidden", TrafficPct: 1}}}
	target, err := ResolveTarget(configs, "Exact", "")
	if err != nil {
		t.Fatal(err)
	}
	coordinator := newExecutionCoordinator(oc, configs, &counter).withTarget(target)
	plan, err := coordinator.plan("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.isCanary || len(plan.enabled) != 2 {
		t.Fatalf("plan %+v", plan)
	}
	seen := make(map[string]bool)
	_, err = coordinator.execute(context.Background(), plan, "", "", nil, func(_ context.Context, info providerInfo, _, _ string) (chatRequestResult, error) {
		if info.name == "forbidden" || info.cfg.Model != "Exact" {
			t.Fatalf("invalid fallback %+v", info)
		}
		seen[info.name] = true
		return chatRequestResult{}, errors.New("fixture failure")
	})
	if err == nil || len(seen) != 2 {
		t.Fatalf("seen=%v err=%v", seen, err)
	}
	if len(coordinator.snapshotConfigs) != 3 || coordinator.snapshotConfigs["first"].Model != "default" {
		t.Fatal("exact target altered publishable snapshot")
	}
}

func TestCancellationStopsKeyRotationAndProviderFailover(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := executeChatAttemptWithKeyRotation(ctx, "first", Config{}, "", "", func(string, chatRequestResult) (bool, error) {
		t.Fatal("rotation after cancellation")
		return false, nil
	}, func(context.Context, string, Config, string, string) (chatRequestResult, error) {
		calls++
		cancel()
		return chatRequestResult{clientKey: "key"}, context.Canceled
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestStreamingConsumerCancellationPreservesOriginalCause(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"fixture\",\"model\":\"model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"text\"},\"finish_reason\":null}]}\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	configs := map[string]Config{"fixture": {BaseURL: server.URL, Model: "model", Enabled: true, APIKey: "fixture"}}
	directory := t.TempDir()
	provider, err := NewOpenAIWithRoutingCallbacks(configs, RoutingConfig{MetricsPath: filepath.Join(directory, "metrics.json"), EventsPath: filepath.Join(directory, "events.jsonl")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.Shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("downstream writer failed")
	_, err = provider.ChatStreamWithInfoContext(ctx, "", "hello", func(ChatStreamChunk) error { cancel(); return cause })
	if !errors.Is(err, cause) || !IsConsumerError(err) {
		t.Fatalf("original write cause replaced: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("consumer failure retried %d times", calls.Load())
	}
	if health := provider.GetOpenAIClients().CircuitBreaker.GetHealth("fixture"); health.Failures != 0 || health.Disabled {
		t.Fatalf("consumer failure penalized: %+v", health)
	}
}

func TestFallbackConsumerCancellationPreservesOriginalCause(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("fallback writer failed")
	breaker := NewCircuitBreaker([]string{"primary", "fallback"}, nil)
	coordinator := executionCoordinator{oc: &OpenAIClients{CircuitBreaker: breaker}}
	plan := chatExecutionPlan{selected: providerInfo{name: "primary"}, enabled: []providerInfo{{name: "primary"}, {name: "fallback"}}}
	calls := 0
	_, err := coordinator.executeStream(ctx, plan, "", "", nil, func(_ context.Context, info providerInfo, _, _ string, _ ChatStreamHandler) (chatRequestResult, bool, error) {
		calls++
		if info.name == "primary" {
			return chatRequestResult{}, false, NewOpenAIError("unauthorized", 401, nil)
		}
		cancel()
		return chatRequestResult{}, true, &ConsumerError{Err: cause}
	})
	if !errors.Is(err, cause) || calls != 2 {
		t.Fatalf("calls=%d cause=%v", calls, err)
	}
	if breaker.GetHealth("fallback").Failures != 0 {
		t.Fatal("fallback consumer penalized")
	}
}

func TestRateLimitAfterCancellationDoesNotRotateKey(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls, rotations := 0, 0
	_, err := executeChatAttemptWithKeyRotation(ctx, "fixture", Config{}, "", "", func(string, chatRequestResult) (bool, error) { rotations++; return true, nil }, func(context.Context, string, Config, string, string) (chatRequestResult, error) {
		calls++
		cancel()
		return chatRequestResult{clientKey: "key"}, NewOpenAIError("rate limited", 429, nil)
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || rotations != 0 {
		t.Fatalf("calls=%d rotations=%d err=%v", calls, rotations, err)
	}
}
