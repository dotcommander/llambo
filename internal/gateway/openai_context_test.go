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
