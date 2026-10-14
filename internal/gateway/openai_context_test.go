package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func TestHandlerChatCompletionStreamUsesOpenAIRequestContext(t *testing.T) {
	maxTokens := 64
	temperature := 0.2
	topP := 0.85
	req := ChatCompletionRequest{
		Messages:    []Message{{Role: "user", Content: "return JSON"}},
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
		TopP:        &topP,
		ResponseFormat: &ResponseFormat{
			Type:       "json_schema",
			JSONSchema: json.RawMessage(`{"name":"answer","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}}`),
		},
		Stream: true,
	}

	parentDeadline := time.Now().Add(time.Minute)
	parent, cancelParent := context.WithDeadline(context.Background(), parentDeadline)
	defer cancelParent()

	server, stream := newHandlerStreamTestServer(t)
	captured := make(chan context.Context, 1)
	stream.onStreamContext = func(ctx context.Context) {
		captured <- ctx
		<-ctx.Done()
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(parent)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.handleChatCompletion(rr, httpReq)
		close(done)
	}()

	gotCtx := <-captured
	if stream.lastStreamCtx != gotCtx {
		t.Fatal("stream mock did not retain its request context")
	}
	assertOpenAIStreamRequestOptions(t, gotCtx)
	if deadline, ok := gotCtx.Deadline(); !ok || deadline.After(parentDeadline) {
		t.Fatalf("stream deadline = %v (present=%t), must not exceed parent deadline", deadline, ok)
	}

	cancelParent()
	select {
	case <-gotCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("request context did not inherit parent cancellation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not return after parent cancellation")
	}
}

func assertOpenAIStreamRequestOptions(t *testing.T, ctx context.Context) {
	t.Helper()
	overrides := providers.ChatRequestOverridesFromContext(ctx)
	if overrides == nil || overrides.MaxTokens == nil || *overrides.MaxTokens != 64 {
		t.Fatalf("max token override = %#v, want 64", overrides)
	}
	if overrides.Temperature == nil || *overrides.Temperature != 0.2 {
		t.Fatalf("temperature override = %#v, want 0.2", overrides)
	}
	if overrides.TopP == nil || *overrides.TopP != 0.85 {
		t.Fatalf("top_p override = %#v, want 0.85", overrides)
	}

	jsonOverrides := providers.JSONOverridesFromContext(ctx)
	generationConfig, ok := jsonOverrides["generationConfig"].(map[string]any)
	if !ok || generationConfig["responseMimeType"] != "application/json" {
		t.Fatalf("generation config = %#v, want JSON response MIME type", jsonOverrides)
	}
	assertOpenAIJSONSchema(t, generationConfig["responseSchema"])

	responseFormat, ok := providers.ResponseFormatOverrideFromContext(ctx).(map[string]any)
	if !ok || responseFormat["type"] != "json_schema" {
		t.Fatalf("response format = %#v, want json_schema", responseFormat)
	}
	if responseFormat["name"] != nil {
		t.Fatalf("response format name must remain under json_schema: %#v", responseFormat)
	}
	assertOpenAIResponseFormatSchema(t, responseFormat["json_schema"])
}

type reasoningOverrideCase struct {
	name        string
	effort      string
	reasoning   json.RawMessage
	wantErr     bool
	wantEffort  string
	wantTokens  int
	wantEnabled *bool
}

func assertReasoningCase(t *testing.T, got *whtypes.Reasoning, want reasoningOverrideCase) {
	t.Helper()
	if want.wantEffort == "" {
		if got != nil {
			t.Fatalf("reasoningOverride() = %+v, want nil", got)
		}
		return
	}
	if got == nil {
		t.Fatalf("reasoningOverride() = nil, want effort %q", want.wantEffort)
	}
	if string(got.Effort) != want.wantEffort {
		t.Fatalf("reasoningOverride() effort = %q, want %q", got.Effort, want.wantEffort)
	}
	if got.MaxTokens != want.wantTokens {
		t.Fatalf("reasoningOverride() max tokens = %d, want %d", got.MaxTokens, want.wantTokens)
	}
	if (got.Enabled == nil) != (want.wantEnabled == nil) {
		t.Fatalf("reasoningOverride() enabled = %+v, want %+v", got.Enabled, want.wantEnabled)
	}
}

func TestReasoningOverride(t *testing.T) {
	tests := []reasoningOverrideCase{
		{
			name:       "effort string only",
			effort:     "high",
			wantEffort: "high",
		},
		{
			name:        "reasoning object",
			reasoning:   json.RawMessage(`{"effort":"low","max_tokens":1024,"enabled":true}`),
			wantEffort:  "low",
			wantTokens:  1024,
			wantEnabled: ptrBool(true),
		},
		{
			name:        "object without effort merges effort string",
			effort:      "medium",
			reasoning:   json.RawMessage(`{"max_tokens":512}`),
			wantEffort:  "medium",
			wantTokens:  512,
			wantEnabled: nil,
		},
		{
			name:       "null reasoning falls back to effort string",
			effort:     "minimal",
			reasoning:  json.RawMessage(`null`),
			wantEffort: "minimal",
		},
		{
			name:      "malformed reasoning object is a request error",
			effort:    "high",
			reasoning: json.RawMessage(`"high"`),
			wantErr:   true,
		},
		{
			name: "no reasoning controls",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := reasoningOverride(ChatCompletionRequest{ReasoningEffort: tt.effort, Reasoning: tt.reasoning})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("reasoningOverride() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("reasoningOverride() error = %v", err)
			}
			assertReasoningCase(t, got, tt)
		})
	}
}

func TestOpenAIRequestContextCarriesReasoning(t *testing.T) {
	t.Parallel()
	req := ChatCompletionRequest{ReasoningEffort: "high"}
	parsed, err := reasoningOverride(req)
	if err != nil {
		t.Fatalf("reasoningOverride() error = %v", err)
	}
	req.nativeReasoning = parsed

	ctx, cancel := openAIRequestContextTimeout(context.Background(), req, time.Second)
	defer cancel()

	got := providers.RequestReasoningFromContext(ctx)
	if got == nil || got.Effort != "high" {
		t.Fatalf("request reasoning from context = %+v, want high effort", got)
	}

	bare, cancelBare := openAIRequestContextTimeout(context.Background(), ChatCompletionRequest{}, time.Second)
	defer cancelBare()
	if providers.RequestReasoningFromContext(bare) != nil {
		t.Fatal("expected no reasoning on context for request without reasoning controls")
	}
}

func ptrBool(v bool) *bool { return &v }

func assertOpenAIJSONSchema(t *testing.T, value any) {
	t.Helper()
	schema, ok := value.(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("response schema = %#v, want object schema", value)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("response schema properties = %#v", schema["properties"])
	}
	okProperty, ok := properties["ok"].(map[string]any)
	if !ok || okProperty["type"] != "boolean" {
		t.Fatalf("response schema ok property = %#v, want boolean", properties["ok"])
	}
}

func assertOpenAIResponseFormatSchema(t *testing.T, value any) {
	t.Helper()
	jsonSchema, ok := value.(map[string]any)
	if !ok || jsonSchema["name"] != "answer" {
		t.Fatalf("response_format json_schema = %#v, want name answer", value)
	}
	assertOpenAIJSONSchema(t, jsonSchema["schema"])
}
