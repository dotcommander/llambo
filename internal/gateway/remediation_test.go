package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func TestExactGatewayOrderedWormholeRequest(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Model    string
			Messages []struct {
				Role       string
				Content    string
				ToolCallID string            `json:"tool_call_id"`
				ToolCalls  []json.RawMessage `json:"tool_calls"`
			}
			Tools []json.RawMessage
			Stop  []string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "Vendor/ExactCase" || len(body.Messages) != 4 || body.Messages[2].Role != "assistant" || len(body.Messages[2].ToolCalls) != 1 || body.Messages[3].ToolCallID != "call1" || len(body.Tools) != 1 || len(body.Stop) != 1 {
			t.Errorf("request lost structure: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "answer", "model": body.Model, "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "done"}, "finish_reason": "stop"}}})
	}))
	t.Cleanup(upstream.Close)
	server := newWormholeHandlerTestServer(t, map[string]providers.Config{"fixture": {Enabled: true, Model: "default", Models: []string{"Vendor/ExactCase"}, BaseURL: upstream.URL, APIKey: "test"}})
	req := ChatCompletionRequest{Model: "fixture/Vendor/ExactCase", Messages: []Message{{Role: "system", Content: "system"}, {Role: "user", Content: "user"}, {Role: "assistant", ToolCalls: []whtypes.ToolCall{{ID: "call1", Type: "function", Function: &whtypes.ToolCallFunction{Name: "lookup", Arguments: "{}"}}}}, {Role: "tool", ToolCallID: "call1", Content: "result"}}, Tools: []whtypes.Tool{{Type: "function", Function: &whtypes.ToolFunction{Name: "lookup", Parameters: map[string]any{"type": "object"}}}}}
	req.Stop, _ = json.Marshal([]string{"halt"})
	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	req.Model = "fixture/missing"
	req.Stream = true
	rr = makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)
	if rr.Code != 400 || calls.Load() != 1 || rr.Header().Get("Content-Type") == "text/event-stream" {
		t.Fatalf("invalid stream target admitted: %d calls=%d", rr.Code, calls.Load())
	}
	rr = makeRequest(t, server.handleCreateJob, "POST", "/v1/jobs", CreateJobRequest{Model: "missing", Requests: []JobRequest{{ID: "1", Messages: []Message{{Role: "user", Content: "hi"}}}}})
	if rr.Code != 400 || calls.Load() != 1 {
		t.Fatalf("invalid job admitted: %d", rr.Code)
	}
}
func TestGatewayAnthropicAuthAuthority(t *testing.T) {
	t.Parallel()
	s := &Server{authRequired: true, authTokenHash: sha256.Sum256([]byte("secret"))}
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("x-api-key", "secret")
	if !s.authorized(req) {
		t.Fatal("x-api-key rejected")
	}
	req.Header.Set("Authorization", "")
	if s.authorized(req) {
		t.Fatal("present invalid authorization ignored")
	}
}
func TestRetentionProtectsDrainingJobsAndEvictsOversized(t *testing.T) {
	t.Parallel()
	m := NewJobManager(context.Background(), newHandlerMockJobQueue(nil))
	t.Cleanup(m.StopCleanup)
	m.SetRetentionLimits(1, 10)
	old := &Job{ID: "old", Status: JobStatusCompleted, UpdatedAt: time.Now().Add(-time.Minute), drained: true, requestBytes: 20}
	draining := &Job{ID: "draining", Status: JobStatusCancelled, UpdatedAt: time.Now().Add(-time.Hour), requestBytes: 20}
	m.jobs[old.ID] = old
	m.jobs[draining.ID] = draining
	m.CleanupOldJobs(JobMaxAge)
	if m.GetJob(old.ID) != nil || m.GetJob(draining.ID) == nil {
		t.Fatal("retention violated drain protection")
	}
	draining.mu.Lock()
	draining.drained = true
	draining.mu.Unlock()
	m.CleanupOldJobs(JobMaxAge)
	if m.GetJob(draining.ID) != nil {
		t.Fatal("oversized drained job retained")
	}
}
func TestAnthropicToolFirstTextBlockIndexes(t *testing.T) {
	t.Parallel()
	state := newAnthropicStreamState("model", nil)
	rr := httptest.NewRecorder()
	first, err := state.ensureToolBlock(rr, rr, providers.ToolCallDelta{Index: 7, ID: "tool1", Name: "lookup"})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.processTextDelta(rr, rr, "text"); err != nil {
		t.Fatal(err)
	}
	if first != 0 || state.textIndex != 1 {
		t.Fatalf("overlapping indexes: tool=%d text=%d", first, state.textIndex)
	}
}
func TestAnthropicStructuredToolHistory(t *testing.T) {
	t.Parallel()
	calls, _ := json.Marshal([]AnthropicContentBlock{{Type: "tool_use", ID: "call", Name: "lookup", Input: json.RawMessage("{}")}})
	results, _ := json.Marshal([]AnthropicContentBlock{{Type: "tool_result", ToolUseID: "call", Content: json.RawMessage(`"result"`)}})
	translated, err := translateAnthropicRequest(AnthropicMessagesRequest{MaxTokens: 32, Messages: []AnthropicInputMessage{{Role: "assistant", Content: calls}, {Role: "user", Content: results}}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := structuredRequest(translated)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 || req.Messages[0].GetRole() != whtypes.RoleAssistant || req.Messages[1].(*whtypes.ToolResultMessage).ToolCallID != "call" {
		t.Fatalf("lost history: %+v", req.Messages)
	}
}

func TestExactGatewayMessagesJobsAndEmbeddingBatches(t *testing.T) {
	t.Parallel()
	var chatCalls, embedCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		var model string
		_ = json.Unmarshal(body["model"], &model)
		if model != "Vendor/ExactCase" {
			t.Errorf("model changed to %q", model)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/embeddings" {
			embedCalls.Add(1)
			var input []string
			_ = json.Unmarshal(body["input"], &input)
			if len(input) != 1 {
				t.Errorf("unexpected batch: %v", input)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "model": model, "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float32{1, 2}}}, "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
			return
		}
		chatCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "answer", "model": model, "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "done"}, "finish_reason": "stop"}}})
	}))
	t.Cleanup(upstream.Close)
	configs := map[string]providers.Config{"fixture": {Enabled: true, Model: "default", Models: []string{"Vendor/ExactCase"}, BaseURL: upstream.URL, APIKey: "test", ProviderType: "openai"}}
	server := newWormholeHandlerTestServer(t, configs)
	embedder, err := providers.NewOpenAIEmbeddingWithConfig(providers.EmbedConfig{Model: "embedding-default", BatchSize: 1, Dimensions: 2}, configs)
	if err != nil {
		t.Fatal(err)
	}
	server.embedder = embedder
	t.Cleanup(embedder.Close)
	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", map[string]any{"model": "fixture/Vendor/ExactCase", "max_tokens": 32, "messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	if rr.Code != 200 || rr.Header().Get("X-Llambo-Model") != "Vendor/ExactCase" {
		t.Fatalf("messages: %d %s", rr.Code, rr.Body.String())
	}
	rr = makeRequest(t, server.handleCreateJob, "POST", "/v1/jobs", CreateJobRequest{Model: "fixture/Vendor/ExactCase", Requests: []JobRequest{{ID: "1", Messages: []Message{{Role: "user", Content: "hi"}}}}})
	if rr.Code != 202 {
		t.Fatalf("job: %d %s", rr.Code, rr.Body.String())
	}
	var admitted JobResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &admitted); err != nil {
		t.Fatal(err)
	}
	job := server.jobManager.GetJob(admitted.JobID)
	if job == nil {
		t.Fatal("job not retained")
	}
	select {
	case <-job.done:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not drain")
	}
	result := job.ToResponse()
	if len(result.Results) != 1 || result.Results[0].Model != "Vendor/ExactCase" {
		t.Fatalf("queue changed model: %+v", result)
	}
	rr = makeRequest(t, server.handleEmbeddings, "POST", "/v1/embeddings", EmbeddingRequest{Model: "fixture/Vendor/ExactCase", Input: []string{"one", "two"}})
	if rr.Code != 200 || embedCalls.Load() != 2 || chatCalls.Load() != 2 {
		t.Fatalf("embedding batches: status %d chat=%d embeddings=%d %s", rr.Code, chatCalls.Load(), embedCalls.Load(), rr.Body.String())
	}
	if rr.Header().Get("X-Llambo-Provider") != "fixture" || rr.Header().Get("X-Llambo-Model") != "Vendor/ExactCase" {
		t.Fatal("missing actual embedding identity")
	}
	for _, path := range []string{"messages", "embeddings"} {
		var body any = EmbeddingRequest{Model: "fixture/missing", Input: []string{"one"}}
		handler := server.handleEmbeddings
		if path == "messages" {
			body = map[string]any{"model": "fixture/missing", "max_tokens": 32, "stream": true, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
			handler = server.handleAnthropicMessages
		}
		rr = makeRequest(t, handler, "POST", "/v1/"+path, body)
		if rr.Code != 400 {
			t.Fatalf("unknown %s: %d", path, rr.Code)
		}
	}
	if chatCalls.Load() != 2 || embedCalls.Load() != 2 {
		t.Fatal("unknown target made upstream call")
	}
}

func TestOpenAIStreamUsageIsOptInAndTerminalDeltaEmpty(t *testing.T) {
	t.Parallel()
	server, stream := newHandlerStreamTestServer(t)
	stream.streamChunks = nil
	stream.streamResult = providers.ChatResult{Provider: "mock-backend", Model: "mock-model", FinishReason: "stop", Usage: &providers.LLMUsage{TotalTokens: 1}}
	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", ChatCompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}, Stream: true})
	if rr.Code != 200 || strings.Contains(rr.Body.String(), `"choices":[]`) || !strings.Contains(rr.Body.String(), `"delta":{},"finish_reason":"stop"`) {
		t.Fatalf("invalid default stream framing: %s", rr.Body.String())
	}
}

func TestJobCancellationAtomicStatuses(t *testing.T) {
	t.Parallel()
	m := NewJobManager(context.Background(), newHandlerMockJobQueue(nil))
	t.Cleanup(m.StopCleanup)
	if _, status := m.CancelJobStatus("missing"); status != 404 {
		t.Fatalf("missing status %d", status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := &Job{ID: "active", Status: JobStatusProcessing, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	m.jobs[job.ID] = job
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() { _, status := m.CancelJobStatus(job.ID); statuses <- status }()
	}
	first, second := <-statuses, <-statuses
	if first+second != http.StatusOK+http.StatusConflict {
		t.Fatalf("concurrent cancellation statuses %d %d", first, second)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("active cancellation did not cancel context")
	}
}

func TestRetentionEvictsOldestFinishedByCount(t *testing.T) {
	t.Parallel()
	m := NewJobManager(context.Background(), newHandlerMockJobQueue(nil))
	t.Cleanup(m.StopCleanup)
	m.SetRetentionLimits(2, 10000)
	now := time.Now()
	for i, id := range []string{"old", "middle", "new"} {
		m.jobs[id] = &Job{ID: id, Status: JobStatusCompleted, drained: true, UpdatedAt: now.Add(time.Duration(i) * time.Second), requestBytes: 2}
	}
	m.CleanupOldJobs(JobMaxAge)
	if m.GetJob("old") != nil || m.GetJob("middle") == nil || m.GetJob("new") == nil {
		t.Fatal("count retention order incorrect")
	}
}

func TestUnsupportedGatewayContentMakesNoCalls(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	t.Cleanup(upstream.Close)
	server := newWormholeHandlerTestServer(t, map[string]providers.Config{"fixture": {Enabled: true, Model: "model", BaseURL: upstream.URL, APIKey: "test"}})
	for _, body := range []string{
		`{"model":"model","stream":true,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image"}}]}]}`,
		`{"model":"model","messages":[{"role":"assistant","tool_calls":[{"id":"call","type":"function","function":{"name":"lookup","arguments":"broken"}}]}]}`,
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		server.handleChatCompletion(rr, req)
		if rr.Code != 400 || rr.Header().Get("Content-Type") == "text/event-stream" {
			t.Fatalf("unsupported content admitted: %d %s", rr.Code, rr.Body.String())
		}
	}
	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", map[string]any{"model": "model", "max_tokens": 32, "stream": true, "messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image"}}}}})
	if rr.Code != 400 || calls.Load() != 0 {
		t.Fatalf("unsupported Anthropic content admitted: %d calls=%d", rr.Code, calls.Load())
	}
}

func TestQueuedRequestToolsOptionsAndOutputs(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{workerCount: 1, processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
		for job := range jobs {
			if job.Request == nil || len(job.Request.Tools) != 1 || job.Request.MaxTokens == nil || *job.Request.MaxTokens != 17 || len(job.Request.Stop) != 1 {
				t.Errorf("queued options lost: %+v", job.Request)
			}
			results <- providers.Result{ID: job.ID, ToolCalls: []providers.ToolCall{{ID: "call", Name: "lookup", Arguments: "{}"}}, FinishReason: "tool_calls"}
		}
	}}
	m := NewJobManager(context.Background(), queue)
	t.Cleanup(m.StopCleanup)
	max := 17
	stop, _ := json.Marshal([]string{"halt"})
	job, err := m.CreateJobIfCapacity(t.Context(), []JobRequest{{ID: "1", Messages: []Message{{Role: "user", Content: "hi"}}, Tools: []whtypes.Tool{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}}, Stop: stop, MaxTokens: &max}}, "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.done:
	case <-time.After(2 * time.Second):
		t.Fatal("queued fixture did not drain")
	}
	result := job.ToResponse()
	if len(result.Results) != 1 || len(result.Results[0].ToolCalls) != 1 || result.Results[0].ToolCalls[0].Function.Name != "lookup" || result.Results[0].FinishReason != "tool_calls" {
		t.Fatalf("queued outputs lost: %+v", result)
	}
}

type gatewayStatusError struct{ status int }

func (e gatewayStatusError) Error() string   { return "unrelated model 429 quota identifier" }
func (e gatewayStatusError) StatusCode() int { return e.status }
func TestAnthropicTypedStatusWinsOverText(t *testing.T) {
	t.Parallel()
	if status := anthropicStatusFromError(fmt.Errorf("wrapped: %w", gatewayStatusError{status: 403})); status != 403 {
		t.Fatalf("typed status lost: %d", status)
	}
	if status := anthropicStatusFromError(fmt.Errorf("model identifier 429, output tokens 401")); status != 502 {
		t.Fatalf("arbitrary numbers classified as HTTP: %d", status)
	}
}
