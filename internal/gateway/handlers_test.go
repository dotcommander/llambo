package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// Test helper functions

func makeRequest(t *testing.T, handler http.HandlerFunc, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&reqBody).Encode(body); err != nil {
			t.Fatalf("Failed to encode request body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, &reqBody)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func decodeResponse[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()

	var resp T
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	return resp
}

type parsedSSEEvent struct {
	Event string
	Data  map[string]any
}

func parseSSEEvents(t *testing.T, body string) []parsedSSEEvent {
	t.Helper()
	lines := strings.Split(body, "\n")
	events := make([]parsedSSEEvent, 0, 8)
	var current parsedSSEEvent
	haveCurrent := false
	for _, line := range lines {
		if strings.HasPrefix(line, "event: ") {
			if haveCurrent {
				events = append(events, current)
			}
			current = parsedSSEEvent{Event: strings.TrimPrefix(line, "event: ")}
			haveCurrent = true
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			if !haveCurrent {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(payload), &m); err != nil {
				t.Fatalf("Failed to parse SSE data payload %q: %v", payload, err)
			}
			current.Data = m
			continue
		}
		if line == "" && haveCurrent {
			events = append(events, current)
			haveCurrent = false
		}
	}
	if haveCurrent {
		events = append(events, current)
	}
	return events
}

func sseIndexField(t *testing.T, data map[string]any) int {
	t.Helper()
	v, ok := data["index"]
	if !ok {
		t.Fatal("SSE event missing index field")
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("SSE event index is not numeric: %#v", v)
	}
	return int(f)
}

type handlerMockStreamChatProvider struct {
	*handlerMockChatProvider
	streamChunks    []providers.ChatStreamChunk
	streamResult    providers.ChatResult
	streamErr       error
	lastStreamCtx   context.Context
	onStreamContext func(context.Context)
}

func (m *handlerMockStreamChatProvider) ChatStreamWithInfoContext(ctx context.Context, systemPrompt, userContent string, onChunk providers.ChatStreamHandler) (providers.ChatResult, error) {
	m.lastStreamCtx = ctx
	if m.onStreamContext != nil {
		m.onStreamContext(ctx)
	}
	for _, ch := range m.streamChunks {
		if onChunk != nil {
			if err := onChunk(ch); err != nil {
				return providers.ChatResult{}, err
			}
		}
	}
	if m.streamErr != nil {
		return providers.ChatResult{}, m.streamErr
	}
	return m.streamResult, nil
}

func newHandlerStreamTestServer(t *testing.T) (*Server, *handlerMockStreamChatProvider) {
	t.Helper()

	base := &handlerMockChatProvider{
		chatResult: providers.ChatResult{
			Content:  "Hello! How can I help you?",
			Provider: "mock-provider",
			Model:    "mock-model",
		},
	}

	mockStream := &handlerMockStreamChatProvider{
		handlerMockChatProvider: base,
		streamResult: providers.ChatResult{
			Content:      "streamed response",
			Provider:     "mock-provider",
			Model:        "mock-model",
			FinishReason: "stop",
			Usage: &providers.LLMUsage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		},
	}

	mockQueue := newHandlerMockJobQueue([]string{"mock-backend"})
	server := &Server{
		provider:   mockStream,
		queue:      mockQueue,
		jobManager: NewJobManager(context.Background(), mockQueue),
		configs: map[string]providers.Config{
			"mock-backend": {Enabled: true, Model: "mock-model", Models: []string{"requested-model", "test-model", "claude-3-5-sonnet", "gemini-2.5-pro", "gemini-2.5-flash"}, Workers: 2},
		},
		startTime: time.Now(),
	}

	t.Cleanup(func() {
		server.jobManager.StopCleanup()
	})

	return server, mockStream
}

// Chat Completion Tests

func TestHandler_ChatCompletion_Success(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatResult = providers.ChatResult{
		Content:  "The answer is 42.",
		Provider: "test-provider",
		Model:    "test-model",
	}

	req := ChatCompletionRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "What is the meaning of life?"},
		},
	}

	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[ChatCompletionResponse](t, rr)

	if resp.Object != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got %q", resp.Object)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("Expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "The answer is 42." {
		t.Errorf("Expected content 'The answer is 42.', got %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got %q", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("Expected finish_reason 'stop', got %q", resp.Choices[0].FinishReason)
	}
	if resp.XLlambo == nil {
		t.Error("Expected x_llambo metadata to be present")
	} else {
		if resp.XLlambo.Backend != "test-provider" {
			t.Errorf("Expected backend 'test-provider', got %q", resp.XLlambo.Backend)
		}
		if resp.XLlambo.Model != "test-model" {
			t.Errorf("Expected model 'test-model', got %q", resp.XLlambo.Model)
		}
	}
	if got := rr.Header().Get("X-Llambo-Provider"); got != "test-provider" {
		t.Errorf("Expected X-Llambo-Provider test-provider, got %q", got)
	}
	if got := rr.Header().Get("X-Llambo-Model"); got != "test-model" {
		t.Errorf("Expected X-Llambo-Model test-model, got %q", got)
	}
}

func TestHandler_ChatCompletion_EmptyMessages(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := ChatCompletionRequest{
		Model:    "test-model",
		Messages: []Message{},
	}

	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request" {
		t.Errorf("Expected error type 'invalid_request', got %q", resp.Error.Type)
	}
	if resp.Error.Message != "Messages array is required" {
		t.Errorf("Expected message 'Messages array is required', got %q", resp.Error.Message)
	}
}

func TestHandler_ChatCompletion_InvalidJSON(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString("{invalid json"))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleChatCompletion(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request" {
		t.Errorf("Expected error type 'invalid_request', got %q", resp.Error.Type)
	}
}

func TestHandler_ChatCompletion_StreamNotSupported(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := ChatCompletionRequest{
		Model:  "test-model",
		Stream: true,
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("Expected status %d, got %d", http.StatusNotImplemented, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "not_implemented" {
		t.Errorf("Expected error type 'not_implemented', got %q", resp.Error.Type)
	}
}

func TestHandler_ChatCompletion_UpstreamError(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatErr = errors.New("upstream provider failed")

	req := ChatCompletionRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("Expected status %d, got %d", http.StatusBadGateway, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "upstream_error" {
		t.Errorf("Expected error type 'upstream_error', got %q", resp.Error.Type)
	}
}

func TestHandler_ChatCompletion_NoContentErrorIncludesFinishReason(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatErr = &providers.NoContentResponseError{
		Model:        "gemini-2.5-pro",
		FinishReason: "MAX_TOKENS",
		Usage: &providers.LLMUsage{
			PromptTokens:     187,
			CompletionTokens: 2045,
			TotalTokens:      2232,
		},
	}

	req := ChatCompletionRequest{
		Model: "gemini-2.5-pro",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("Expected status %d, got %d", http.StatusBadGateway, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "no_content" {
		t.Fatalf("Expected error type 'no_content', got %q", resp.Error.Type)
	}
	if resp.Error.FinishReason != "length" {
		t.Fatalf("finish_reason = %q, want length", resp.Error.FinishReason)
	}
	if resp.Error.PromptTokens != 187 || resp.Error.CompletionTokens != 2045 || resp.Error.TotalTokens != 2232 {
		t.Fatalf("usage = %d/%d/%d, want 187/2045/2232", resp.Error.PromptTokens, resp.Error.CompletionTokens, resp.Error.TotalTokens)
	}
}

func TestHandler_AnthropicMessages_Success(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatResult = providers.ChatResult{
		Content:  "The answer is 42.",
		Provider: "test-provider",
		Model:    "test-model",
		Usage: &providers.LLMUsage{
			PromptTokens:     12,
			CompletionTokens: 7,
			TotalTokens:      19,
		},
	}

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 256,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What is the meaning of life?"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[AnthropicMessagesResponse](t, rr)
	if resp.Type != "message" {
		t.Errorf("Expected type 'message', got %q", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got %q", resp.Role)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("Expected content type 'text', got %q", resp.Content[0].Type)
	}
	if resp.Content[0].Text != "The answer is 42." {
		t.Errorf("Expected content text 'The answer is 42.', got %q", resp.Content[0].Text)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("Expected stop_reason 'end_turn', got %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 7 {
		t.Errorf("Expected usage 12/7, got %d/%d", resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
	if got := rr.Header().Get("anthropic-version"); got != defaultAnthropicVersion {
		t.Fatalf("Expected default anthropic-version %q, got %q", defaultAnthropicVersion, got)
	}
}

func TestHandler_AnthropicMessages_UsesIncomingAnthropicVersionHeader(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	var reqBody bytes.Buffer
	if err := json.NewEncoder(&reqBody).Encode(map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 64,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hello"},
		},
	}); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/messages", &reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	rr := httptest.NewRecorder()
	server.handleAnthropicMessages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if got := rr.Header().Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("Expected anthropic-version response header to mirror request, got %q", got)
	}
}

func TestHandler_AnthropicMessages_StrictModeRequiresAnthropicVersionHeader(t *testing.T) {
	t.Setenv("LLAMBO_ANTHROPIC_STRICT", "true")
	server, _, _ := newHandlerTestServer(t)

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 64,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request_error" {
		t.Fatalf("Expected invalid_request_error, got %q", resp.Error.Type)
	}
}

func TestHandler_AnthropicMessages_PragmaUnknownFieldsAllowed(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := map[string]interface{}{
		"model":         "claude-3-5-sonnet",
		"max_tokens":    64,
		"unknown_field": "ignored",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestHandler_AnthropicMessages_StrictUnknownFieldsRejected(t *testing.T) {
	t.Setenv("LLAMBO_ANTHROPIC_STRICT", "true")
	server, _, _ := newHandlerTestServer(t)

	var reqBody bytes.Buffer
	if err := json.NewEncoder(&reqBody).Encode(map[string]interface{}{
		"model":         "claude-3-5-sonnet",
		"max_tokens":    64,
		"unknown_field": "rejected",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hello"},
		},
	}); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/messages", &reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	rr := httptest.NewRecorder()
	server.handleAnthropicMessages(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request_error" {
		t.Fatalf("Expected invalid_request_error, got %q", resp.Error.Type)
	}
}

func TestHandler_AnthropicMessages_InvalidBlockType(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "video", "source": map[string]interface{}{"type": "url", "url": "https://example.com/x.mp4"}},
				},
			},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Type != "error" {
		t.Errorf("Expected type 'error', got %q", resp.Type)
	}
	if resp.Error.Type != "invalid_request_error" {
		t.Errorf("Expected error type 'invalid_request_error', got %q", resp.Error.Type)
	}
	if resp.RequestID == "" {
		t.Error("Expected request_id to be present")
	}
}

func TestHandler_AnthropicMessages_InvalidToolBlockShape(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]interface{}{
			{
				"role": "assistant",
				"content": []map[string]interface{}{
					{"type": "tool_use", "name": "search", "input": map[string]interface{}{"query": "llambo"}},
				},
			},
			{"role": "user", "content": "continue"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request_error" {
		t.Fatalf("Expected invalid_request_error, got %q", resp.Error.Type)
	}
}

func TestHandler_AnthropicMessages_ToolChoiceWithoutTools(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := map[string]interface{}{
		"model":       "claude-3-5-sonnet",
		"max_tokens":  128,
		"tool_choice": map[string]interface{}{"type": "any"},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request_error" {
		t.Fatalf("Expected invalid_request_error, got %q", resp.Error.Type)
	}
}

func TestHandler_AnthropicMessages_InvalidOptionalFields(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"top_p":      1.5,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request_error" {
		t.Fatalf("Expected invalid_request_error, got %q", resp.Error.Type)
	}
}

func TestHandler_AnthropicMessages_ForwardsMetadataAndOverrides(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)

	req := map[string]interface{}{
		"model":       "claude-3-5-sonnet",
		"max_tokens":  222,
		"temperature": 0.3,
		"top_p":       0.85,
		"top_k":       64,
		"thinking":    map[string]interface{}{"type": "enabled", "budget": 1024},
		"metadata": map[string]interface{}{
			"trace_id": "abc123",
			"tags":     []string{"x", "y"},
		},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if mockChat.lastCtx == nil {
		t.Fatal("Expected provider context capture")
	}

	ov := providers.ChatRequestOverridesFromContext(mockChat.lastCtx)
	if ov == nil || ov.MaxTokens == nil || *ov.MaxTokens != 222 {
		t.Fatalf("Expected max token override 222, got %+v", ov)
	}
	if ov.Temperature == nil || *ov.Temperature != 0.3 {
		t.Fatalf("Expected temperature override 0.3, got %+v", ov)
	}
	if ov.TopP == nil || *ov.TopP != 0.85 {
		t.Fatalf("Expected top_p override 0.85, got %+v", ov)
	}

	meta := providers.RequestMetadataFromContext(mockChat.lastCtx)
	if meta["trace_id"] != "abc123" {
		t.Fatalf("Expected metadata trace_id=abc123, got %+v", meta)
	}
	if !strings.Contains(meta["tags"], "x") || !strings.Contains(meta["tags"], "y") {
		t.Fatalf("Expected metadata tags to preserve array data, got %+v", meta)
	}

	jsonOverrides := providers.JSONOverridesFromContext(mockChat.lastCtx)
	if jsonOverrides["top_k"] != float64(64) && jsonOverrides["top_k"] != 64 {
		t.Fatalf("Expected top_k JSON override 64, got %+v", jsonOverrides["top_k"])
	}
	if _, ok := jsonOverrides["thinking"]; !ok {
		t.Fatalf("Expected thinking JSON override, got %+v", jsonOverrides)
	}
}

func TestHandler_ChatCompletion_ResponseFormatJSONAddsGeminiGenerationConfig(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)

	req := map[string]interface{}{
		"model":       "gemini-2.5-flash",
		"max_tokens":  64,
		"temperature": 0.2,
		"response_format": map[string]interface{}{
			"type": "json_object",
		},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Return {\"ok\":true}."},
		},
	}

	rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	jsonOverrides := providers.JSONOverridesFromContext(mockChat.lastCtx)
	generationConfig, ok := jsonOverrides["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("Expected generationConfig JSON override, got %+v", jsonOverrides)
	}
	if generationConfig["responseMimeType"] != "application/json" {
		t.Fatalf("Expected responseMimeType application/json, got %+v", generationConfig)
	}
	if generationConfig["maxOutputTokens"] != float64(64) && generationConfig["maxOutputTokens"] != 64 {
		t.Fatalf("Expected maxOutputTokens 64, got %+v", generationConfig)
	}
	if generationConfig["temperature"] != 0.2 {
		t.Fatalf("Expected temperature 0.2, got %+v", generationConfig)
	}
}

func TestHandler_AnthropicMessages_StopSequenceEnforced(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatResult = providers.ChatResult{
		Content:      "Hello END ignore this",
		Provider:     "test-provider",
		Model:        "test-model",
		FinishReason: "stop",
	}

	req := map[string]interface{}{
		"model":          "claude-3-5-sonnet",
		"max_tokens":     128,
		"stop_sequences": []string{"", "END", "END"},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[AnthropicMessagesResponse](t, rr)
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello " {
		t.Fatalf("Expected content truncated by stop sequence, got %+v", resp.Content)
	}
	if resp.StopReason != "stop_sequence" {
		t.Fatalf("Expected stop_reason stop_sequence, got %q", resp.StopReason)
	}
	if resp.StopSequence == nil || *resp.StopSequence != "END" {
		t.Fatalf("Expected stop_sequence END, got %+v", resp.StopSequence)
	}
}

func TestHandler_AnthropicMessages_PrefillAssistantBestEffort(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatResult = providers.ChatResult{
		Content:  "42)",
		Provider: "test-provider",
		Model:    "test-model",
	}

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Question"},
			{"role": "assistant", "content": "The answer is ("},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[AnthropicMessagesResponse](t, rr)
	if len(resp.Content) != 1 || resp.Content[0].Text != "42)" {
		t.Fatalf("Unexpected content response: %+v", resp.Content)
	}
}

func TestHandler_AnthropicMessages_StreamNotSupported(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"stream":     true,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("Expected status %d, got %d", http.StatusNotImplemented, rr.Code)
	}

	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "api_error" {
		t.Errorf("Expected error type 'api_error', got %q", resp.Error.Type)
	}
}

func TestHandler_AnthropicMessages_StreamSuccess_TextDeltas(t *testing.T) {
	t.Parallel()
	server, mockStream := newHandlerStreamTestServer(t)
	mockStream.streamChunks = []providers.ChatStreamChunk{
		{ContentDelta: "Hello "},
		{ContentDelta: "world"},
		{FinishReason: "stop"},
	}

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"stream":     true,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Expected Content-Type text/event-stream, got %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Fatalf("Expected message_start event, body: %s", body)
	}
	if !strings.Contains(body, "event: content_block_start") || !strings.Contains(body, "event: content_block_delta") {
		t.Fatalf("Expected content block stream events, body: %s", body)
	}
	if !strings.Contains(body, "event: message_delta") || !strings.Contains(body, "event: message_stop") {
		t.Fatalf("Expected message terminal events, body: %s", body)
	}
	if !strings.Contains(body, `"stop_reason":"end_turn"`) {
		t.Fatalf("Expected end_turn stop reason in stream body: %s", body)
	}
}

func TestHandler_AnthropicMessages_StreamEventOrderConformance(t *testing.T) {
	t.Parallel()
	server, mockStream := newHandlerStreamTestServer(t)
	mockStream.streamChunks = []providers.ChatStreamChunk{
		{ContentDelta: "Hello"},
		{FinishReason: "stop"},
	}
	mockStream.streamResult.FinishReason = "stop"

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 64,
		"stream":     true,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hi"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	events := parseSSEEvents(t, rr.Body.String())
	if len(events) < 6 {
		t.Fatalf("Expected at least 6 SSE events, got %d", len(events))
	}
	wantPrefix := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	for i, want := range wantPrefix {
		if events[i].Event != want {
			t.Fatalf("Unexpected event at index %d: got %q want %q", i, events[i].Event, want)
		}
	}
	startIdx := sseIndexField(t, events[1].Data)
	deltaIdx := sseIndexField(t, events[2].Data)
	stopIdx := sseIndexField(t, events[3].Data)
	if startIdx != 0 || deltaIdx != 0 || stopIdx != 0 {
		t.Fatalf("Expected text block events on index 0, got start=%d delta=%d stop=%d", startIdx, deltaIdx, stopIdx)
	}
}

func TestHandler_AnthropicMessages_StreamSuccess_ToolDeltas(t *testing.T) {
	t.Parallel()
	server, mockStream := newHandlerStreamTestServer(t)
	mockStream.streamChunks = []providers.ChatStreamChunk{
		{
			ToolDeltas: []providers.ToolCallDelta{{Index: 0, ID: "call_1", Name: "search"}},
		},
		{
			ToolDeltas: []providers.ToolCallDelta{{Index: 0, ArgumentsDelta: `{"q":"llambo"}`}},
		},
		{FinishReason: "tool_calls"},
	}
	mockStream.streamResult.FinishReason = "tool_calls"

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"stream":     true,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Find llambo"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, `"type":"tool_use"`) {
		t.Fatalf("Expected tool_use block in stream body: %s", body)
	}
	if !strings.Contains(body, `"type":"input_json_delta"`) {
		t.Fatalf("Expected input_json_delta event in stream body: %s", body)
	}
	if !strings.Contains(body, `"stop_reason":"tool_use"`) {
		t.Fatalf("Expected tool_use stop reason in stream body: %s", body)
	}
}

func TestHandler_AnthropicMessages_StreamToolBlockIndexConformance(t *testing.T) {
	t.Parallel()
	server, mockStream := newHandlerStreamTestServer(t)
	mockStream.streamChunks = []providers.ChatStreamChunk{
		{ToolDeltas: []providers.ToolCallDelta{{Index: 0, ID: "call_1", Name: "search"}}},
		{ToolDeltas: []providers.ToolCallDelta{{Index: 1, ID: "call_2", Name: "lookup"}}},
		{ToolDeltas: []providers.ToolCallDelta{{Index: 0, ArgumentsDelta: `{"q":"one"}`}, {Index: 1, ArgumentsDelta: `{"q":"two"}`}}},
		{FinishReason: "tool_calls"},
	}
	mockStream.streamResult.FinishReason = "tool_calls"

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"stream":     true,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "do two tool calls"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	events := parseSSEEvents(t, rr.Body.String())
	var toolStarts []int
	for _, ev := range events {
		if ev.Event != "content_block_start" {
			continue
		}
		cbRaw, ok := ev.Data["content_block"]
		if !ok {
			continue
		}
		cb, ok := cbRaw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := cb["type"].(string)
		if typ == "tool_use" {
			toolStarts = append(toolStarts, sseIndexField(t, ev.Data))
		}
	}
	if len(toolStarts) != 2 {
		t.Fatalf("Expected 2 tool_use block starts, got %d", len(toolStarts))
	}
	if toolStarts[0] != 0 || toolStarts[1] != 1 {
		t.Fatalf("Expected tool block indices [0 1], got %+v", toolStarts)
	}

	body := rr.Body.String()
	if !strings.Contains(body, `"stop_reason":"tool_use"`) {
		t.Fatalf("Expected tool_use stop reason in stream body: %s", body)
	}
}

func TestHandler_AnthropicMessages_StreamErrorEvent(t *testing.T) {
	t.Parallel()
	server, mockStream := newHandlerStreamTestServer(t)
	mockStream.streamChunks = []providers.ChatStreamChunk{{ContentDelta: "partial", Provider: "mock-provider", Model: "mock-model"}}
	mockStream.streamErr = providers.NewOpenAIError("rate limited", http.StatusTooManyRequests, errors.New("429"))

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"stream":     true,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("Expected error event in stream, body: %s", body)
	}
	if !strings.Contains(body, `"type":"rate_limit_error"`) {
		t.Fatalf("Expected rate_limit_error in stream error event, body: %s", body)
	}
	if !strings.Contains(body, `"message":"Upstream provider rate limit exceeded"`) {
		t.Fatalf("Expected sanitized stream error message, body: %s", body)
	}
	if strings.Contains(body, `"message":"rate limited"`) {
		t.Fatalf("Raw upstream stream error leaked, body: %s", body)
	}
}

func TestHandler_AnthropicMessages_StreamStopSequenceEnforced(t *testing.T) {
	t.Parallel()
	server, mockStream := newHandlerStreamTestServer(t)
	mockStream.streamChunks = []providers.ChatStreamChunk{
		{ContentDelta: "Hello E"},
		{ContentDelta: "ND world"},
		{FinishReason: "stop"},
	}
	mockStream.streamResult.FinishReason = "stop"

	req := map[string]interface{}{
		"model":          "claude-3-5-sonnet",
		"max_tokens":     128,
		"stream":         true,
		"stop_sequences": []string{"END"},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"text":"Hello"`) {
		t.Fatalf("Expected streamed text prefix before stop sequence, body: %s", body)
	}
	if strings.Contains(body, "world") {
		t.Fatalf("Did not expect text after stop sequence, body: %s", body)
	}
	if !strings.Contains(body, `"stop_reason":"stop_sequence"`) || !strings.Contains(body, `"stop_sequence":"END"`) {
		t.Fatalf("Expected stop_sequence termination in stream body: %s", body)
	}
}

func TestHandler_AnthropicMessages_RejectsTooManyStopSequences(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	stops := make([]string, maxAnthropicStopSequences+1)
	for i := range stops {
		stops[i] = "END"
	}

	req := map[string]interface{}{
		"model":          "claude-3-5-sonnet",
		"max_tokens":     128,
		"stop_sequences": stops,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request_error" {
		t.Fatalf("Expected invalid_request_error, got %q", resp.Error.Type)
	}
}

func TestHandler_AnthropicMessages_UpstreamError(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatErr = errors.New("upstream provider failed")

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("Expected status %d, got %d", http.StatusBadGateway, rr.Code)
	}

	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "api_error" {
		t.Errorf("Expected error type 'api_error', got %q", resp.Error.Type)
	}
	if resp.Error.Message != "Upstream provider request failed" {
		t.Errorf("Expected sanitized error message, got %q", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, "upstream provider failed") {
		t.Errorf("Raw upstream error leaked: %q", resp.Error.Message)
	}
}

func TestHandler_AnthropicMessages_UpstreamRateLimitStatusMapping(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatErr = providers.NewOpenAIError("rate limited", http.StatusTooManyRequests, errors.New("429"))

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected status %d, got %d", http.StatusTooManyRequests, rr.Code)
	}

	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "rate_limit_error" {
		t.Fatalf("Expected rate_limit_error, got %q", resp.Error.Type)
	}
}

func TestHandler_AnthropicMessages_UpstreamWrappedStatusMapping(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatErr = errors.New("all providers failed: openai: status 503 unavailable")

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}

	resp := decodeResponse[AnthropicErrorResponse](t, rr)
	if resp.Error.Type != "overloaded_error" {
		t.Fatalf("Expected overloaded_error for 503 mapping, got %q", resp.Error.Type)
	}
}

func TestHandler_AnthropicMessages_ToolCallResponseMapping(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatResult = providers.ChatResult{
		Provider:     "test-provider",
		Model:        "test-model",
		FinishReason: "tool_calls",
		ToolCalls: []providers.ToolCall{
			{ID: "call_1", Name: "search", Arguments: `{"q":"llambo"}`},
		},
	}

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Find llambo"},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[AnthropicMessagesResponse](t, rr)
	if resp.StopReason != "tool_use" {
		t.Fatalf("Expected stop_reason tool_use, got %q", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "tool_use" {
		t.Fatalf("Expected tool_use content block, got %+v", resp.Content)
	}
	if resp.Content[0].ID != "call_1" || resp.Content[0].Name != "search" {
		t.Fatalf("Unexpected tool block ids/names: %+v", resp.Content[0])
	}
}

func TestHandler_AnthropicMessages_AcceptsToolResultInput(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatResult = providers.ChatResult{
		Content:  "Tool result received.",
		Provider: "test-provider",
		Model:    "test-model",
	}

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]interface{}{
			{
				"role": "assistant",
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "toolu_123", "name": "search", "input": map[string]interface{}{"query": "llambo"}},
				},
			},
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "tool_result", "tool_use_id": "toolu_123", "content": []map[string]interface{}{{"type": "text", "text": "Top hit: llambo README"}}},
				},
			},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[AnthropicMessagesResponse](t, rr)
	if len(resp.Content) == 0 || resp.Content[0].Type != "text" {
		t.Fatalf("Expected text response content, got %+v", resp.Content)
	}
}

func TestHandler_AnthropicMessages_AcceptsToolResultJSONContent(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatResult = providers.ChatResult{
		Content:  "Processed structured tool output.",
		Provider: "test-provider",
		Model:    "test-model",
	}

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 128,
		"messages": []map[string]interface{}{
			{
				"role": "assistant",
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "toolu_456", "name": "lookup", "input": map[string]interface{}{"id": 99}},
				},
			},
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "tool_result", "tool_use_id": "toolu_456", "content": map[string]interface{}{"status": "ok", "items": []string{"a", "b"}}},
				},
			},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[AnthropicMessagesResponse](t, rr)
	if len(resp.Content) == 0 || resp.Content[0].Type != "text" {
		t.Fatalf("Expected text response content, got %+v", resp.Content)
	}
}

func TestHandler_AnthropicMessages_AcceptsMultiTurnToolLoopPayload(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatResult = providers.ChatResult{
		Content:  "Based on the tool output, here is the final answer.",
		Provider: "test-provider",
		Model:    "test-model",
	}

	req := map[string]interface{}{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 256,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Find the latest release date."},
			{
				"role": "assistant",
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "toolu_789", "name": "search", "input": map[string]interface{}{"query": "llambo latest release"}},
				},
			},
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "tool_result", "tool_use_id": "toolu_789", "content": "Release v1.2.3 on 2026-02-01"},
				},
			},
			{"role": "user", "content": "Now summarize in one sentence."},
		},
	}

	rr := makeRequest(t, server.handleAnthropicMessages, "POST", "/v1/messages", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[AnthropicMessagesResponse](t, rr)
	if len(resp.Content) == 0 || resp.Content[0].Type != "text" {
		t.Fatalf("Expected text response content, got %+v", resp.Content)
	}
}

// Job Tests

func TestHandler_CreateJob_Success(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := CreateJobRequest{
		SystemPrompt: "Be helpful",
		Requests: []JobRequest{
			{ID: "req-1", Messages: []Message{{Role: "user", Content: "Question 1"}}},
			{ID: "req-2", Messages: []Message{{Role: "user", Content: "Question 2"}}},
		},
	}

	rr := makeRequest(t, server.handleCreateJob, "POST", "/v1/jobs", req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, rr.Code)
	}

	resp := decodeResponse[JobResponse](t, rr)
	if resp.JobID == "" {
		t.Error("Expected job ID to be non-empty")
	}
	if resp.Status != string(JobStatusPending) {
		t.Errorf("Expected status %q, got %q", JobStatusPending, resp.Status)
	}
	if resp.Total != 2 {
		t.Errorf("Expected total 2, got %d", resp.Total)
	}
	if resp.Completed != 0 {
		t.Errorf("Expected completed 0, got %d", resp.Completed)
	}
}

func TestHandler_CreateJob_InvalidJSON(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := httptest.NewRequest("POST", "/v1/jobs", bytes.NewBufferString("{invalid json"))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleCreateJob(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request" {
		t.Errorf("Expected error type 'invalid_request', got %q", resp.Error.Type)
	}
}

func TestHandler_CreateJob_EmptyRequests(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	req := CreateJobRequest{
		SystemPrompt: "Be helpful",
		Requests:     []JobRequest{},
	}

	rr := makeRequest(t, server.handleCreateJob, "POST", "/v1/jobs", req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request" {
		t.Errorf("Expected error type 'invalid_request', got %q", resp.Error.Type)
	}
}

func TestHandler_CreateJob_TooManyRequests(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	requests := make([]JobRequest, JobMaxRequestsPerJob+1)
	for i := range requests {
		requests[i] = JobRequest{
			ID:       "req",
			Messages: []Message{{Role: "user", Content: "Question"}},
		}
	}

	req := CreateJobRequest{SystemPrompt: "Be helpful", Requests: requests}
	rr := makeRequest(t, server.handleCreateJob, "POST", "/v1/jobs", req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request" {
		t.Fatalf("Expected error type invalid_request, got %q", resp.Error.Type)
	}
}

func TestHandler_CreateJob_QueueSaturated(t *testing.T) {
	t.Parallel()
	blockCh := make(chan struct{})
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			<-blockCh
			for job := range jobs {
				results <- providers.Result{ID: job.ID, Content: "done"}
			}
		},
	}

	jobManager := NewJobManager(context.Background(), queue)
	jobManager.SetMaxActiveJobs(1)
	defer jobManager.StopCleanup()

	server := &Server{
		provider:   &handlerMockChatProvider{},
		queue:      queue,
		jobManager: jobManager,
		configs: map[string]providers.Config{
			"mock": {Enabled: true, Model: "mock-model", Workers: 1},
		},
		startTime: time.Now(),
	}

	seedReq := []JobRequest{{ID: "seed", Messages: []Message{{Role: "user", Content: "seed"}}}}
	if _, err := server.jobManager.CreateJobIfCapacity(t.Context(), seedReq, "System"); err != nil {
		t.Fatalf("failed to seed active job: %v", err)
	}

	req := CreateJobRequest{Requests: []JobRequest{{ID: "req-1", Messages: []Message{{Role: "user", Content: "q"}}}}}
	rr := makeRequest(t, server.handleCreateJob, "POST", "/v1/jobs", req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "queue_saturated" {
		t.Fatalf("expected error type queue_saturated, got %q", resp.Error.Type)
	}

	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on saturation")
	}

	close(blockCh)
}

func TestHandler_GetJob_Success(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	// First create a job
	createReq := CreateJobRequest{
		Requests: []JobRequest{
			{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
		},
	}
	createRR := makeRequest(t, server.handleCreateJob, "POST", "/v1/jobs", createReq)
	createResp := decodeResponse[JobResponse](t, createRR)

	// Now get the job - need to use a mux to test PathValue
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/jobs/{id}", server.handleGetJob)

	req := httptest.NewRequest("GET", "/v1/jobs/"+createResp.JobID, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[JobResponse](t, rr)
	if resp.JobID != createResp.JobID {
		t.Errorf("Expected job ID %q, got %q", createResp.JobID, resp.JobID)
	}
}

func TestHandler_GetJob_NotFound(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/jobs/{id}", server.handleGetJob)

	req := httptest.NewRequest("GET", "/v1/jobs/nonexistent-job", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "not_found" {
		t.Errorf("Expected error type 'not_found', got %q", resp.Error.Type)
	}
}

func TestHandler_CancelJob_Success(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	// Create a job
	createReq := CreateJobRequest{
		Requests: []JobRequest{
			{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
		},
	}
	createRR := makeRequest(t, server.handleCreateJob, "POST", "/v1/jobs", createReq)
	createResp := decodeResponse[JobResponse](t, createRR)

	// Cancel it immediately (before it completes)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", server.handleCancelJob)

	req := httptest.NewRequest("POST", "/v1/jobs/"+createResp.JobID+"/cancel", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// Note: The job might complete before we cancel it, so we check for either success or already completed
	if rr.Code != http.StatusOK && rr.Code != http.StatusConflict {
		t.Errorf("Expected status %d or %d, got %d", http.StatusOK, http.StatusConflict, rr.Code)
	}
}

func TestHandler_CancelJob_NotFound(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", server.handleCancelJob)

	req := httptest.NewRequest("POST", "/v1/jobs/nonexistent-job/cancel", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "not_found" {
		t.Errorf("Expected error type 'not_found', got %q", resp.Error.Type)
	}
}

// Health and Providers Tests

func TestHandler_Health(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	rr := makeRequest(t, server.handleHealth, "GET", "/health", nil)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[HealthResponse](t, rr)
	if resp.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %q", resp.Status)
	}
	if resp.UptimeSeconds < 0 {
		t.Errorf("Expected non-negative uptime, got %d", resp.UptimeSeconds)
	}
	if len(resp.Backends) == 0 {
		t.Error("Expected at least one backend in health response")
	}
}

func TestHandler_Health_Degraded(t *testing.T) {
	t.Parallel()
	server, _, mockQueue := newHandlerTestServer(t)

	// Simulate failures to make backend unhealthy
	for i := 0; i < 5; i++ {
		mockQueue.circuitBreaker.RecordFailure("mock-backend", errors.New("test error"))
	}

	rr := makeRequest(t, server.handleHealth, "GET", "/health", nil)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[HealthResponse](t, rr)
	if resp.Status != "degraded" {
		t.Errorf("Expected status 'degraded', got %q", resp.Status)
	}

	backend, ok := resp.Backends["mock-backend"]
	if !ok {
		t.Fatal("Expected mock-backend in health response")
	}
	if backend.Healthy {
		t.Error("Expected mock-backend to be unhealthy")
	}
	if backend.Failures != 5 {
		t.Errorf("Expected 5 failures, got %d", backend.Failures)
	}
}

func TestHandler_Providers(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	rr := makeRequest(t, server.handleProviders, "GET", "/providers", nil)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[ProvidersResponse](t, rr)
	if len(resp.Providers) != 1 {
		t.Errorf("Expected 1 provider, got %d", len(resp.Providers))
	}
	if len(resp.Providers) > 0 {
		p := resp.Providers[0]
		if p.Name != "mock-backend" {
			t.Errorf("Expected provider name 'mock-backend', got %q", p.Name)
		}
		if p.Model != "mock-model" {
			t.Errorf("Expected model 'mock-model', got %q", p.Model)
		}
		if !p.Enabled {
			t.Error("Expected provider to be enabled")
		}
	}
}

// Models Tests

func TestHandler_ListModels(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)

	rr := makeRequest(t, server.handleListModels, "GET", "/v1/models", nil)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[ModelsResponse](t, rr)
	if resp.Object != "list" {
		t.Errorf("Expected object 'list', got %q", resp.Object)
	}
	if len(resp.Data) != 1 {
		t.Errorf("Expected 1 model, got %d", len(resp.Data))
	}
	if len(resp.Data) > 0 {
		model := resp.Data[0]
		if model.ID != "mock-model" {
			t.Errorf("Expected model ID 'mock-model', got %q", model.ID)
		}
		if model.Object != "model" {
			t.Errorf("Expected object 'model', got %q", model.Object)
		}
	}
}

// Embeddings Tests

func TestHandler_Embeddings_Success(t *testing.T) {
	t.Parallel()
	server, mockEmbed := newHandlerTestServerWithEmbeddings(t)
	mockEmbed.embeddings = [][]float32{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	}

	req := EmbeddingRequest{
		Model: "text-embedding-test",
		Input: []string{"Hello", "World"},
	}

	rr := makeRequest(t, server.handleEmbeddings, "POST", "/v1/embeddings", req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	resp := decodeResponse[EmbeddingResponse](t, rr)
	if resp.Object != "list" {
		t.Errorf("Expected object 'list', got %q", resp.Object)
	}
	if len(resp.Data) != 2 {
		t.Errorf("Expected 2 embeddings, got %d", len(resp.Data))
	}
	if resp.Model != "text-embedding-test" {
		t.Errorf("Expected model 'text-embedding-test', got %q", resp.Model)
	}
}

func TestHandler_Embeddings_StringInput(t *testing.T) {
	t.Parallel()
	server, mockEmbed := newHandlerTestServerWithEmbeddings(t)
	mockEmbed.embeddings = [][]float32{{0.1, 0.2, 0.3}}

	body := `{"model":"text-embedding-test","input":"Hello"}`
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleEmbeddings(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
	resp := decodeResponse[EmbeddingResponse](t, rr)
	if len(resp.Data) != 1 {
		t.Errorf("Expected 1 embedding, got %d", len(resp.Data))
	}
}

func TestHandler_Embeddings_NotConfigured(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)
	// Don't set embedder, leaving it nil

	req := EmbeddingRequest{
		Model: "text-embedding-test",
		Input: []string{"Hello"},
	}

	rr := makeRequest(t, server.handleEmbeddings, "POST", "/v1/embeddings", req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "not_configured" {
		t.Errorf("Expected error type 'not_configured', got %q", resp.Error.Type)
	}
}

func TestHandler_Embeddings_EmptyInput(t *testing.T) {
	t.Parallel()
	server, _ := newHandlerTestServerWithEmbeddings(t)

	req := EmbeddingRequest{
		Model: "text-embedding-test",
		Input: []string{},
	}

	rr := makeRequest(t, server.handleEmbeddings, "POST", "/v1/embeddings", req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "invalid_request" {
		t.Errorf("Expected error type 'invalid_request', got %q", resp.Error.Type)
	}
}

func TestHandler_Embeddings_InvalidJSON(t *testing.T) {
	t.Parallel()
	server, _ := newHandlerTestServerWithEmbeddings(t)

	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewBufferString("{invalid json"))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleEmbeddings(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestHandler_Embeddings_UpstreamError(t *testing.T) {
	t.Parallel()
	server, mockEmbed := newHandlerTestServerWithEmbeddings(t)
	mockEmbed.embedErr = errors.New("embedding execution failed")

	req := EmbeddingRequest{
		Model: "text-embedding-test",
		Input: []string{"Hello"},
	}

	rr := makeRequest(t, server.handleEmbeddings, "POST", "/v1/embeddings", req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("Expected status %d, got %d", http.StatusBadGateway, rr.Code)
	}

	resp := decodeResponse[ErrorResponse](t, rr)
	if resp.Error.Type != "upstream_error" {
		t.Errorf("Expected error type 'upstream_error', got %q", resp.Error.Type)
	}
}

// Table-driven tests for chat completion edge cases

func TestHandler_ChatCompletion_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		request        ChatCompletionRequest
		setupMock      func(*handlerMockChatProvider)
		expectedStatus int
		expectedType   string // error type if error expected
	}{
		{
			name: "with system message",
			request: ChatCompletionRequest{
				Model: "test",
				Messages: []Message{
					{Role: "system", Content: "You are helpful"},
					{Role: "user", Content: "Hello"},
				},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "multiple user messages",
			request: ChatCompletionRequest{
				Model: "test",
				Messages: []Message{
					{Role: "user", Content: "First"},
					{Role: "assistant", Content: "Response"},
					{Role: "user", Content: "Second"},
				},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "nil messages",
			request: ChatCompletionRequest{
				Model:    "test",
				Messages: nil,
			},
			expectedStatus: http.StatusBadRequest,
			expectedType:   "invalid_request",
		},
		{
			name: "with temperature and max_tokens",
			request: ChatCompletionRequest{
				Model:       "test",
				Messages:    []Message{{Role: "user", Content: "Hello"}},
				Temperature: func() *float64 { v := 0.7; return &v }(),
				MaxTokens:   func() *int { v := 100; return &v }(),
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server, mockChat, _ := newHandlerTestServer(t)
			mockChat.chatResult = providers.ChatResult{
				Content:  "Test response",
				Provider: "test",
				Model:    "test",
			}

			if tt.setupMock != nil {
				tt.setupMock(mockChat)
			}

			rr := makeRequest(t, server.handleChatCompletion, "POST", "/v1/chat/completions", tt.request)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			if tt.expectedType != "" {
				resp := decodeResponse[ErrorResponse](t, rr)
				if resp.Error.Type != tt.expectedType {
					t.Errorf("Expected error type %q, got %q", tt.expectedType, resp.Error.Type)
				}
			}
		})
	}
}

func (m *handlerMockStreamChatProvider) ChatStructuredStreamWithInfoContext(ctx context.Context, req providers.StructuredChatRequest, target providers.ResolvedTarget, onChunk providers.ChatStreamHandler) (providers.ChatResult, error) {
	return m.ChatStreamWithInfoContext(ctx, "", "", onChunk)
}
