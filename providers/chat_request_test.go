package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func TestBuildTextRequest_AttachesStopSequencesFromContext(t *testing.T) {
	t.Parallel()
	ctx := WithStopSequences(context.Background(), []string{"END", "STOP"})
	req := buildTextRequest(ctx, Config{Model: "gpt-4o"}, "system", "user")

	if len(req.Stop) != 2 {
		t.Fatalf("expected 2 stop sequences, got %d", len(req.Stop))
	}
	if req.Stop[0] != "END" || req.Stop[1] != "STOP" {
		t.Fatalf("unexpected stop sequences: %+v", req.Stop)
	}
}

func TestBuildTextRequest_SystemPromptPlacementByProvider(t *testing.T) {
	t.Parallel()
	openAIReq := buildTextRequest(context.Background(), Config{Model: "gpt-4o", ProviderType: "openai"}, "system", "user")
	if len(openAIReq.Messages) != 2 || openAIReq.Messages[0].GetRole() != whtypes.RoleSystem {
		t.Fatalf("expected OpenAI-compatible request to include system message, got %+v", openAIReq.Messages)
	}

	geminiReq := buildTextRequest(context.Background(), Config{Model: "gemini-2.0-flash", ProviderType: "gemini"}, "system", "user")
	if len(geminiReq.Messages) != 1 || geminiReq.Messages[0].GetRole() != whtypes.RoleUser || geminiReq.SystemPrompt != "system" {
		t.Fatalf("expected Gemini request to keep system prompt separate, got messages=%+v system=%q", geminiReq.Messages, geminiReq.SystemPrompt)
	}
}

func TestBuildTextRequest_TruncatesStopSequencesToNativeLimit(t *testing.T) {
	t.Parallel()
	ctx := WithStopSequences(context.Background(), []string{"a", "b", "c", "d", "e"})
	req := buildTextRequest(ctx, Config{Model: "gpt-4o"}, "system", "user")

	if len(req.Stop) != maxNativeStopSequences {
		t.Fatalf("expected %d stop sequences, got %d", maxNativeStopSequences, len(req.Stop))
	}
}

func TestWithStopSequences_CopiesInput(t *testing.T) {
	t.Parallel()
	in := []string{"x", "y"}
	ctx := WithStopSequences(context.Background(), in)
	in[0] = "mutated"

	out := StopSequencesFromContext(ctx)
	if out[0] != "x" {
		t.Fatalf("expected stored sequence to be immutable copy, got %q", out[0])
	}
	out[1] = "changed"
	again := StopSequencesFromContext(ctx)
	if again[1] != "y" {
		t.Fatalf("expected extracted sequence to be copy, got %q", again[1])
	}
}

func TestIsUnsupportedStopError(t *testing.T) {
	t.Parallel()
	if !isUnsupportedStopError(assertErr("stop not supported for this model")) {
		t.Fatal("expected unsupported stop error to be detected")
	}
	if isUnsupportedStopError(assertErr("rate limit exceeded")) {
		t.Fatal("did not expect unrelated error to be detected")
	}
}

func TestExtractContentFromTextResponse_ToolCallOnly(t *testing.T) {
	t.Parallel()
	resp := &whtypes.TextResponse{
		FinishReason: whtypes.FinishReasonToolCalls,
		ToolCalls: []whtypes.ToolCall{{
			ID:   "call_1",
			Name: "search",
			Function: &whtypes.ToolCallFunction{
				Name:      "search",
				Arguments: `{"q":"llambo"}`,
			},
		}},
	}

	content, _, finish, toolCalls, err := extractContentFromTextResponse(resp, "gpt-4o")
	if err != nil {
		t.Fatalf("expected no error for tool-call-only response, got %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty content, got %q", content)
	}
	if finish != "tool_calls" {
		t.Fatalf("expected finish reason tool_calls, got %q", finish)
	}
	if len(toolCalls) != 1 || toolCalls[0].ID != "call_1" || toolCalls[0].Name != "search" {
		t.Fatalf("unexpected tool calls: %+v", toolCalls)
	}
}

func TestExtractContentFromTextResponsePreservesServedIdentity(t *testing.T) {
	t.Parallel()
	resp := &whtypes.TextResponse{Provider: "wire-provider", Model: "served-model", Text: "ok", FinishReason: whtypes.FinishReasonStop}
	_, _, _, _, identity, err := extractContentFromTextResponseWithIdentity(resp, "requested-model")
	if err != nil {
		t.Fatal(err)
	}
	if identity.provider != "wire-provider" || identity.model != "served-model" {
		t.Fatalf("response identity = %#v", identity)
	}
}

func TestBuildTextRequest_AttachesToolsAndToolChoice(t *testing.T) {
	t.Parallel()
	ctx := WithTools(context.Background(), []ToolDefinition{{
		Name:        "search",
		Description: "search docs",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
	}}, &ToolChoice{Type: "tool", Name: "search"})

	req := buildTextRequest(ctx, Config{Model: "gpt-4o"}, "system", "user")
	if len(req.Tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(req.Tools))
	}
	if req.Tools[0].Name != "search" || req.Tools[0].Function == nil || req.Tools[0].Function.Name != "search" {
		t.Fatalf("expected tool name search, got %+v", req.Tools[0])
	}
	if req.ToolChoice == nil || req.ToolChoice.Type != whtypes.ToolChoiceTypeSpecific || req.ToolChoice.ToolName != "search" {
		t.Fatalf("expected specific tool choice for search, got %+v", req.ToolChoice)
	}
}

func TestBuildTextRequest_UsesRequestOverrides(t *testing.T) {
	t.Parallel()
	maxTokens := 111
	temperature := 0.7
	topP := 0.8
	ctx := WithChatRequestOverrides(context.Background(), &maxTokens, &temperature, &topP)

	req := buildTextRequest(ctx, Config{Model: "gpt-4o", MaxTokens: 22, Temperature: 0.1}, "system", "user")
	if req.MaxTokens == nil || *req.MaxTokens != 111 {
		t.Fatalf("expected max tokens override 111, got %+v", req.MaxTokens)
	}
	if req.Temperature == nil || *req.Temperature != float32(0.7) {
		t.Fatalf("expected temperature override 0.7, got %+v", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != float32(0.8) {
		t.Fatalf("expected top_p override 0.8, got %+v", req.TopP)
	}
}

func TestBuildTextRequest_PreservesExplicitZeroTemperature(t *testing.T) {
	t.Parallel()
	temperature := 0.0
	req := buildTextRequest(WithChatRequestOverrides(context.Background(), nil, &temperature, nil), Config{Model: "gpt-4o", Temperature: 0.7}, "system", "user")
	if req.Temperature == nil || *req.Temperature != 0 {
		t.Fatalf("expected explicit zero temperature pointer, got %+v", req.Temperature)
	}
}

func TestBuildTextRequest_ForwardsFullResponseFormatWithoutMutatingGenerationConfig(t *testing.T) {
	t.Parallel()
	configuredGeneration := map[string]any{"candidateCount": 1}
	cfg := Config{
		Model:        "gemini-2.5-flash",
		ProviderType: "gemini",
		ExtraBody:    map[string]any{"generationConfig": configuredGeneration},
	}
	override := map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "answer",
			"strict": true,
			"schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}},
		},
	}
	ctx := WithResponseFormatOverride(context.Background(), override)
	ctx = WithJSONOverrides(ctx, map[string]any{"generationConfig": map[string]any{"responseMimeType": "application/json"}})
	req := buildTextRequest(ctx, cfg, "system", "user")

	responseFormat, ok := req.ResponseFormat.(map[string]any)
	if !ok || responseFormat["type"] != "json_schema" {
		t.Fatalf("expected full response format, got %#v", req.ResponseFormat)
	}
	jsonSchema := responseFormat["json_schema"].(map[string]any)
	if jsonSchema["name"] != "answer" || jsonSchema["strict"] != true {
		t.Fatalf("response format lost json_schema fields: %#v", jsonSchema)
	}
	generationConfig := req.ProviderOptions["generationConfig"].(map[string]any)
	if generationConfig["candidateCount"] != 1 || generationConfig["responseMimeType"] != "application/json" {
		t.Fatalf("generation config was not merged: %#v", generationConfig)
	}
	generationConfig["candidateCount"] = 2
	if configuredGeneration["candidateCount"] != 1 {
		t.Fatalf("request mutation altered configured generation config: %#v", configuredGeneration)
	}
}

func TestBuildTextRequest_MergesProviderOptions(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Model:     "gpt-4o",
		ExtraBody: map[string]any{"a": 1, "b": "x"},
		ExtraBodyByModel: map[string]map[string]any{
			"gpt-4o": {"b": "model", "reasoning": map[string]any{"effort": "minimal"}},
		},
	}
	ctx := WithRequestMetadata(context.Background(), map[string]string{"trace_id": "abc123", "tenant": "demo"})
	ctx = WithJSONOverrides(ctx, map[string]any{"top_k": 50, "a": 2})

	req := buildTextRequest(ctx, cfg, "system", "user")
	if req.ProviderOptions["a"] != 2 {
		t.Fatalf("expected context override for a, got %+v", req.ProviderOptions["a"])
	}
	if req.ProviderOptions["b"] != "model" {
		t.Fatalf("expected model override for b, got %+v", req.ProviderOptions["b"])
	}
	if req.ProviderOptions["top_k"] != 50 {
		t.Fatalf("expected top_k override, got %+v", req.ProviderOptions["top_k"])
	}
	meta, ok := req.ProviderOptions["metadata"].(map[string]string)
	if !ok || meta["trace_id"] != "abc123" || meta["tenant"] != "demo" {
		t.Fatalf("unexpected metadata: %+v", req.ProviderOptions["metadata"])
	}
}

func TestBuildTextRequest_ForwardsGeminiJSONGenerationConfig(t *testing.T) {
	t.Parallel()
	ctx := WithJSONOverrides(context.Background(), map[string]any{
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
		},
	})

	req := buildTextRequest(ctx, Config{Model: "gemini-2.5-flash", ProviderType: "gemini"}, "system", "user")
	generationConfig, ok := req.ProviderOptions["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected generationConfig provider option, got %+v", req.ProviderOptions)
	}
	if generationConfig["responseMimeType"] != "application/json" {
		t.Fatalf("responseMimeType = %v, want application/json", generationConfig["responseMimeType"])
	}
}

func TestUsageOrEstimatePreservesProviderUsage(t *testing.T) {
	t.Parallel()
	providerUsage := &LLMUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}
	got := usageOrEstimate(providerUsage, "system", "user", "content")
	if got != providerUsage {
		t.Fatal("expected provider-supplied usage to be preserved")
	}
}

func TestUsageOrEstimateFillsMissingUsage(t *testing.T) {
	t.Parallel()
	got := usageOrEstimate(nil, "system prompt", "user prompt", "4")
	if got == nil {
		t.Fatal("expected estimated usage")
	}
	if got.PromptTokens <= 0 || got.CompletionTokens <= 0 || got.TotalTokens != got.PromptTokens+got.CompletionTokens {
		t.Fatalf("unexpected estimated usage: %+v", got)
	}
}

type staticErr string

func (e staticErr) Error() string { return string(e) }

func assertErr(s string) error { return staticErr(s) }

func TestExecuteChatRequest_EmitsStructuredEntryExitLogs(t *testing.T) {
	// Mutates the global slog default logger; must not run in parallel.
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	client := &fakeTextProvider{resp: &whtypes.TextResponse{
		Text:         "hello",
		FinishReason: whtypes.FinishReasonStop,
		Usage:        &whtypes.Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
	}}

	content, _, _, _, err := ExecuteChatRequest(
		context.Background(),
		client,
		Config{Model: "test-model-1", ProviderType: "openai"},
		"SENTINEL_SYSTEM_PROMPT",
		"super-secret-user-prompt",
		DefaultChatConfig,
	)
	if err != nil {
		t.Fatalf("ExecuteChatRequest: %v", err)
	}
	if content != "hello" {
		t.Fatalf("unexpected content: %q", content)
	}

	logs := buf.String()
	for _, want := range []string{"provider chat request: entry", "provider chat request: exit", `"provider":"openai"`, `"model":"test-model-1"`, `"outcome":"success"`, `"retry_count":0`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("expected log output to contain %q, got: %s", want, logs)
		}
	}
	// latency must be emitted as a structured attr.
	if !strings.Contains(logs, `"latency":`) {
		t.Fatalf("expected latency attr in logs, got: %s", logs)
	}
	// PII must never appear in logs.
	if strings.Contains(logs, "super-secret-user-prompt") || strings.Contains(logs, "SENTINEL_SYSTEM_PROMPT") {
		t.Fatalf("log output leaked prompt content: %s", logs)
	}
}

type fakeTextProvider struct {
	resp *whtypes.TextResponse
	err  error
}

func (f *fakeTextProvider) Name() string                                     { return "fake" }
func (f *fakeTextProvider) Close() error                                     { return nil }
func (f *fakeTextProvider) SupportedCapabilities() []whtypes.ModelCapability { return nil }
func (f *fakeTextProvider) Text(_ context.Context, _ whtypes.TextRequest) (*whtypes.TextResponse, error) {
	return f.resp, f.err
}
func (f *fakeTextProvider) Stream(_ context.Context, _ whtypes.TextRequest) (<-chan whtypes.TextChunk, error) {
	return nil, nil
}
func (f *fakeTextProvider) Structured(_ context.Context, _ whtypes.StructuredRequest) (*whtypes.StructuredResponse, error) {
	return nil, nil
}
func (f *fakeTextProvider) Embeddings(_ context.Context, _ whtypes.EmbeddingsRequest) (*whtypes.EmbeddingsResponse, error) {
	return nil, nil
}
func (f *fakeTextProvider) Rerank(_ context.Context, _ whtypes.RerankRequest) (*whtypes.RerankResponse, error) {
	return nil, nil
}
func (f *fakeTextProvider) Audio(_ context.Context, _ whtypes.AudioRequest) (*whtypes.AudioResponse, error) {
	return nil, nil
}
func (f *fakeTextProvider) SpeechToText(_ context.Context, _ whtypes.SpeechToTextRequest) (*whtypes.SpeechToTextResponse, error) {
	return nil, nil
}
func (f *fakeTextProvider) TextToSpeech(_ context.Context, _ whtypes.TextToSpeechRequest) (*whtypes.TextToSpeechResponse, error) {
	return nil, nil
}
func (f *fakeTextProvider) Images(_ context.Context, _ whtypes.ImagesRequest) (*whtypes.ImagesResponse, error) {
	return nil, nil
}
func (f *fakeTextProvider) GenerateImage(_ context.Context, _ whtypes.ImageRequest) (*whtypes.ImageResponse, error) {
	return nil, nil
}
