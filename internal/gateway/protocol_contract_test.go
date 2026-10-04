package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dotcommander/llambo/providers"
)

func TestHandler_ChatCompletion_ForwardsFullResponseFormat(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	req := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "return JSON"}},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "answer", "strict": true,
				"schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}},
			},
		},
	}
	rr := makeRequest(t, server.handleChatCompletion, http.MethodPost, "/v1/chat/completions", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	got, ok := providers.ResponseFormatOverrideFromContext(mockChat.lastCtx).(map[string]any)
	if !ok || got["type"] != "json_schema" {
		t.Fatalf("response format = %#v", got)
	}
	jsonSchema := got["json_schema"].(map[string]any)
	if jsonSchema["name"] != "answer" || jsonSchema["strict"] != true {
		t.Fatalf("json_schema fields were not forwarded: %#v", jsonSchema)
	}
}

func TestHandler_ChatCompletion_UsageMatchesXLlambo(t *testing.T) {
	t.Parallel()
	server, mockChat, _ := newHandlerTestServer(t)
	mockChat.chatResult.Usage = &providers.LLMUsage{
		PromptTokens:     7,
		CompletionTokens: 5,
		TotalTokens:      12,
	}
	req := ChatCompletionRequest{Messages: []Message{{Role: "user", Content: "hello"}}}
	rr := makeRequest(t, server.handleChatCompletion, http.MethodPost, "/v1/chat/completions", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var response ChatCompletionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Usage == nil || response.Usage.PromptTokens != 7 || response.Usage.CompletionTokens != 5 || response.Usage.TotalTokens != 12 {
		t.Fatalf("standard usage = %#v", response.Usage)
	}
	if response.XLlambo == nil ||
		response.XLlambo.PromptTokens != response.Usage.PromptTokens ||
		response.XLlambo.CompletionTokens != response.Usage.CompletionTokens ||
		response.XLlambo.TotalTokens != response.Usage.TotalTokens {
		t.Fatalf("x_llambo usage = %#v, standard usage = %#v", response.XLlambo, response.Usage)
	}
}

func TestHandler_ChatCompletionStream_UsesActualChunkMetadata(t *testing.T) {
	t.Parallel()
	server, stream := newHandlerStreamTestServer(t)
	stream.streamChunks = []providers.ChatStreamChunk{{ContentDelta: "hello", Provider: "routed-backend", Model: "routed-model"}}
	req := ChatCompletionRequest{Model: "requested-model", Messages: []Message{{Role: "user", Content: "hello"}}, Stream: true, StreamOptions: &StreamOptions{IncludeUsage: true}}
	rr := makeRequest(t, server.handleChatCompletion, http.MethodPost, "/v1/chat/completions", req)
	if got := rr.Header().Get("X-Llambo-Provider"); got != "routed-backend" {
		t.Fatalf("provider header = %q, want routed-backend", got)
	}
	if got := rr.Header().Get("X-Llambo-Model"); got != "routed-model" {
		t.Fatalf("model header = %q, want routed-model", got)
	}
}

func TestHandler_ChatCompletionStream_ZeroDeltasUseResultMetadata(t *testing.T) {
	t.Parallel()
	server, stream := newHandlerStreamTestServer(t)
	stream.streamChunks = nil
	stream.streamResult = providers.ChatResult{Provider: "routed-backend", Model: "routed-model", FinishReason: "stop"}
	req := ChatCompletionRequest{Model: "requested-model", Messages: []Message{{Role: "user", Content: "hello"}}, Stream: true, StreamOptions: &StreamOptions{IncludeUsage: true}}
	rr := makeRequest(t, server.handleChatCompletion, http.MethodPost, "/v1/chat/completions", req)
	if got := rr.Header().Get("X-Llambo-Model"); got != "routed-model" {
		t.Fatalf("model header = %q, want routed-model", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"choices":[]`) || !strings.Contains(body, `"backend":"routed-backend"`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("final metadata chunk missing: %s", body)
	}
}

func TestHandler_ChatCompletionStream_ZeroDeltaRolePrecedesTerminalEvents(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		chunks []providers.ChatStreamChunk
	}{
		{name: "result finish", chunks: nil},
		{name: "callback finish", chunks: []providers.ChatStreamChunk{{FinishReason: "stop"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, stream := newHandlerStreamTestServer(t)
			stream.streamChunks = tc.chunks
			stream.streamResult = providers.ChatResult{Provider: "routed-backend", Model: "routed-model", FinishReason: "stop"}
			req := ChatCompletionRequest{Model: "requested-model", Messages: []Message{{Role: "user", Content: "hello"}}, Stream: true, StreamOptions: &StreamOptions{IncludeUsage: true}}
			rr := makeRequest(t, server.handleChatCompletion, http.MethodPost, "/v1/chat/completions", req)

			events := strings.Split(strings.TrimSpace(rr.Body.String()), "\n\n")
			if len(events) != 4 {
				t.Fatalf("SSE event count = %d, want 4: %s", len(events), rr.Body.String())
			}
			if events[3] != "data: [DONE]" {
				t.Fatalf("final event = %q, want data: [DONE]", events[3])
			}

			var role, finish, metadata map[string]any
			for i, target := range []*map[string]any{&role, &finish, &metadata} {
				if err := json.Unmarshal([]byte(strings.TrimPrefix(events[i], "data: ")), target); err != nil {
					t.Fatalf("decode event %d: %v", i, err)
				}
			}
			roleChoice := role["choices"].([]any)[0].(map[string]any)
			if got := roleChoice["delta"].(map[string]any)["role"]; got != "assistant" {
				t.Fatalf("role chunk = %#v, want assistant", roleChoice)
			}
			finishChoice := finish["choices"].([]any)[0].(map[string]any)
			if got := finishChoice["finish_reason"]; got != "stop" {
				t.Fatalf("finish chunk = %#v, want stop", finishChoice)
			}
			if choices := metadata["choices"].([]any); len(choices) != 0 || metadata["x_llambo"] == nil {
				t.Fatalf("metadata chunk = %#v, want empty choices with x_llambo", metadata)
			}
		})
	}
}

func TestHandler_AnthropicMessagesStream_MessageStartUsesRoutedModel(t *testing.T) {
	t.Parallel()
	server, stream := newHandlerStreamTestServer(t)
	stream.streamChunks = []providers.ChatStreamChunk{{ContentDelta: "hello", Provider: "routed-backend", Model: "routed-model"}}
	req := map[string]any{"model": "requested-model", "max_tokens": 8, "stream": true, "messages": []map[string]any{{"role": "user", "content": "hello"}}}
	rr := makeRequest(t, server.handleAnthropicMessages, http.MethodPost, "/v1/messages", req)
	events := parseSSEEvents(t, rr.Body.String())
	if len(events) == 0 || events[0].Event != "message_start" {
		t.Fatalf("message_start missing: %s", rr.Body.String())
	}
	message := events[0].Data["message"].(map[string]any)
	if message["model"] != "routed-model" {
		t.Fatalf("message_start model = %#v, want routed-model", message["model"])
	}
	if got := rr.Header().Get("X-Llambo-Provider"); got != "routed-backend" {
		t.Fatalf("provider header = %q, want routed-backend", got)
	}
}

func TestAnthropicStreamState_DoesNotSplitUTF8WhileBufferingStops(t *testing.T) {
	t.Parallel()
	state := newAnthropicStreamState("model", []string{"END"})
	rr := newStreamStateRecorder()
	if err := state.processTextDelta(rr, rr, "hi \xe2"); err != nil {
		t.Fatalf("first delta: %v", err)
	}
	if err := state.processTextDelta(rr, rr, "\x98\x83 there"); err != nil {
		t.Fatalf("second delta: %v", err)
	}
	if err := state.flushPendingText(rr, rr); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if strings.Contains(rr.Body.String(), "�") || !strings.Contains(rr.Body.String(), "☃") {
		t.Fatalf("stream split a UTF-8 rune: %s", rr.Body.String())
	}
}

func TestDecodeSingleJSON_RequiresEOF(t *testing.T) {
	t.Parallel()
	var req ChatCompletionRequest
	if err := decodeSingleJSON(strings.NewReader(`{"messages":[]} {}`), &req, false); err == nil {
		t.Fatal("expected concatenated JSON values to fail")
	}
	if err := decodeSingleJSON(strings.NewReader("{\"messages\":[]}  \n\t"), &req, false); err != nil {
		t.Fatalf("trailing whitespace must be accepted: %v", err)
	}
}

func TestValidateJobBatch_Contracts(t *testing.T) {
	t.Parallel()
	valid := []JobRequest{{ID: " id ", Messages: []Message{{Role: "system", Content: "system"}, {Role: "user", Content: "question"}, {Role: "assistant", Content: "answer"}}}}
	if err := validateJobBatch(valid); err != nil {
		t.Fatalf("valid batch: %v", err)
	}
	for _, tc := range []struct {
		name string
		in   []JobRequest
		want string
	}{
		{"blank id", []JobRequest{{ID: " ", Messages: valid[0].Messages}}, "requests[0].id"},
		{"exact duplicate", []JobRequest{{ID: "a", Messages: valid[0].Messages}, {ID: "a", Messages: valid[0].Messages}}, "requests[1].id"},
		{"missing messages", []JobRequest{{ID: "a"}}, "requests[0].messages"},
		{"invalid role", []JobRequest{{ID: "a", Messages: []Message{{Role: "tool", Content: "x"}}}}, "requests[0].messages[0].tool_call_id"},
		{"blank content", []JobRequest{{ID: "a", Messages: []Message{{Role: "user", Content: " "}}}}, "requests[0].messages[0].content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateJobBatch(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want field %q", err, tc.want)
			}
		})
	}
}

func TestHandler_CreateJob_ShutdownErrorCode(t *testing.T) {
	t.Parallel()
	server, _, _ := newHandlerTestServer(t)
	server.jobManager.BeginShutdown()
	req := CreateJobRequest{Requests: []JobRequest{{
		ID:       "request-1",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}}}
	rr := makeRequest(t, server.handleCreateJob, http.MethodPost, "/v1/jobs", req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rr.Body.String(), `"type":"server_shutting_down"`) {
		t.Fatalf("response missing server_shutting_down error: %s", rr.Body.String())
	}
}

type streamStateRecorder struct{ *testResponseRecorder }

// newStreamStateRecorder avoids the response recorder implementation
// owned by a concurrent test file while still supplying http.Flusher.
func newStreamStateRecorder() *streamStateRecorder {
	return &streamStateRecorder{testResponseRecorder: newTestResponseRecorder()}
}

func (r *streamStateRecorder) Header() http.Header { return r.testResponseRecorder.Header() }
func (r *streamStateRecorder) Write(p []byte) (int, error) {
	return r.testResponseRecorder.Write(p)
}
func (r *streamStateRecorder) WriteHeader(statusCode int) {
	r.testResponseRecorder.WriteHeader(statusCode)
}
func (r *streamStateRecorder) Flush() {}

type testResponseRecorder struct {
	header http.Header
	Body   bytes.Buffer
}

func newTestResponseRecorder() *testResponseRecorder {
	return &testResponseRecorder{header: make(http.Header)}
}
func (r *testResponseRecorder) Header() http.Header         { return r.header }
func (r *testResponseRecorder) Write(p []byte) (int, error) { return r.Body.Write(p) }
func (r *testResponseRecorder) WriteHeader(int)             {}
