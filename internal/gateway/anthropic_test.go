package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dotcommander/llambo/providers"
)

func TestTranslateAnthropicRequest_StringAndBlocks(t *testing.T) {
	t.Parallel()
	topP := 0.9
	req := AnthropicMessagesRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 256,
		TopP:      &topP,
		System:    json.RawMessage(`[{"type":"text","text":"Be concise."}]`),
		Messages: []AnthropicInputMessage{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"Hi"}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"Tell me more"}]`)},
		},
	}

	out, err := translateAnthropicRequest(req)
	if err != nil {
		t.Fatalf("translateAnthropicRequest error: %v", err)
	}

	if len(out.Messages) != 4 {
		t.Fatalf("Expected 4 translated messages, got %d", len(out.Messages))
	}
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "Be concise." {
		t.Fatalf("Unexpected system message: %+v", out.Messages[0])
	}
	if out.Messages[1].Role != "user" || out.Messages[1].Content != "Hello" {
		t.Fatalf("Unexpected first user message: %+v", out.Messages[1])
	}
	if out.Messages[2].Role != "assistant" || out.Messages[2].Content != "Hi" {
		t.Fatalf("Unexpected assistant message: %+v", out.Messages[2])
	}
	if out.Messages[3].Role != "user" || out.Messages[3].Content != "Tell me more" {
		t.Fatalf("Unexpected final user message: %+v", out.Messages[3])
	}
	if out.MaxTokens == nil || *out.MaxTokens != 256 {
		t.Fatalf("Expected max_tokens 256, got %+v", out.MaxTokens)
	}
	if out.TopP == nil || *out.TopP != topP {
		t.Fatalf("Expected top_p %.1f, got %+v", topP, out.TopP)
	}
}

func TestTranslateAnthropicRequest_RejectsTooManyMessages(t *testing.T) {
	t.Parallel()
	messages := make([]AnthropicInputMessage, maxAnthropicMessages+1)
	for i := range messages {
		messages[i] = AnthropicInputMessage{Role: "user", Content: json.RawMessage(`"x"`)}
	}

	_, err := translateAnthropicRequest(AnthropicMessagesRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 64,
		Messages:  messages,
	})
	if err == nil {
		t.Fatal("Expected error for too many messages")
	}
}

func TestTranslateAnthropicRequest_RejectsTooManyContentBlocks(t *testing.T) {
	t.Parallel()
	blocks := make([]AnthropicContentBlock, maxAnthropicContentBlocks+1)
	for i := range blocks {
		blocks[i] = AnthropicContentBlock{Type: "text", Text: "x"}
	}

	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("Marshal blocks: %v", err)
	}

	_, err = translateAnthropicRequest(AnthropicMessagesRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 64,
		Messages: []AnthropicInputMessage{{
			Role:    "user",
			Content: json.RawMessage(raw),
		}},
	})
	if err == nil {
		t.Fatal("Expected error for too many content blocks")
	}
}

func TestTranslateAnthropicRequest_RejectsImageBlocks(t *testing.T) {
	t.Parallel()
	req := AnthropicMessagesRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 128,
		Messages: []AnthropicInputMessage{
			{Role: "user", Content: json.RawMessage(`[{"type":"image","source":{"type":"url","url":"https://example.com/x.png"}},{"type":"tool_use","id":"toolu_1","name":"search","input":{"q":"llambo"}}]`)},
		},
	}

	_, err := translateAnthropicRequest(req)
	if err == nil {
		t.Fatal("Expected error for image content blocks")
	}
	if !strings.Contains(err.Error(), "image") {
		t.Fatalf("Expected image-related error, got: %v", err)
	}
}

func TestTranslateAnthropicRequest_AppendsContinuationForFinalAssistantPrefill(t *testing.T) {
	t.Parallel()
	req := AnthropicMessagesRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 128,
		Messages: []AnthropicInputMessage{
			{Role: "user", Content: json.RawMessage(`"Question"`)},
			{Role: "assistant", Content: json.RawMessage(`"The answer is ("`)},
		},
	}

	out, err := translateAnthropicRequest(req)
	if err != nil {
		t.Fatalf("Expected successful translation, got error: %v", err)
	}
	if len(out.Messages) != 3 {
		t.Fatalf("Expected 3 messages after synthetic continuation prompt, got %d", len(out.Messages))
	}
	last := out.Messages[len(out.Messages)-1]
	if last.Role != "user" {
		t.Fatalf("Expected synthetic user message, got role %q", last.Role)
	}
	if last.Content != anthropicPrefillContinuationPrompt {
		t.Fatalf("Unexpected synthetic continuation prompt: %q", last.Content)
	}
}

func TestBuildAnthropicResponse(t *testing.T) {
	t.Parallel()
	res := buildAnthropicResponse(providers.ChatResult{
		Content: "Hello!",
		Model:   "gpt-4o",
		Usage: &providers.LLMUsage{
			PromptTokens:     11,
			CompletionTokens: 5,
			TotalTokens:      16,
		},
	}, nil)

	if res.Type != "message" || res.Role != "assistant" {
		t.Fatalf("Unexpected type/role: %s/%s", res.Type, res.Role)
	}
	if len(res.Content) != 1 || res.Content[0].Type != "text" || res.Content[0].Text != "Hello!" {
		t.Fatalf("Unexpected content: %+v", res.Content)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("Expected stop_reason end_turn, got %q", res.StopReason)
	}
	if res.Usage.InputTokens != 11 || res.Usage.OutputTokens != 5 {
		t.Fatalf("Unexpected usage: %+v", res.Usage)
	}
}

func TestBuildAnthropicResponse_StopReasonMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		reason string
		want   string
	}{
		{name: "stop maps to end_turn", reason: "stop", want: "end_turn"},
		{name: "length maps to max_tokens", reason: "length", want: "max_tokens"},
		{name: "tool_calls maps to tool_use", reason: "tool_calls", want: "tool_use"},
		{name: "function_call maps to tool_use", reason: "function_call", want: "tool_use"},
		{name: "unknown defaults to end_turn", reason: "content_filter", want: "end_turn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := buildAnthropicResponse(providers.ChatResult{Content: "x", FinishReason: tt.reason}, nil)
			if res.StopReason != tt.want {
				t.Fatalf("StopReason: got %q, want %q", res.StopReason, tt.want)
			}
		})
	}
}

func TestBuildAnthropicResponse_StopSequenceEnforced(t *testing.T) {
	t.Parallel()
	res := buildAnthropicResponse(
		providers.ChatResult{Content: "hello END trailing", FinishReason: "stop"},
		[]string{"END"},
	)

	if len(res.Content) != 1 || res.Content[0].Text != "hello " {
		t.Fatalf("Expected content truncated before stop sequence, got: %+v", res.Content)
	}
	if res.StopReason != "stop_sequence" {
		t.Fatalf("Expected stop_reason stop_sequence, got %q", res.StopReason)
	}
	if res.StopSequence == nil || *res.StopSequence != "END" {
		t.Fatalf("Expected stop_sequence END, got %+v", res.StopSequence)
	}
}

func TestTranslateAnthropicRequest_RejectsUnknownBlockType(t *testing.T) {
	t.Parallel()
	req := AnthropicMessagesRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 128,
		Messages: []AnthropicInputMessage{
			{Role: "user", Content: json.RawMessage(`[{"type":"video","url":"https://example.com/v.mp4"}]`)},
		},
	}

	_, err := translateAnthropicRequest(req)
	if err == nil {
		t.Fatal("Expected error for unknown block type")
	}
}

func TestTranslateAnthropicRequest_RejectsToolUseMissingIDOrName(t *testing.T) {
	t.Parallel()
	reqMissingID := AnthropicMessagesRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 128,
		Messages: []AnthropicInputMessage{
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","name":"search","input":{"q":"llambo"}}]`)},
			{Role: "user", Content: json.RawMessage(`"continue"`)},
		},
	}
	if _, err := translateAnthropicRequest(reqMissingID); err == nil {
		t.Fatal("Expected error for tool_use missing id")
	}

	reqMissingName := AnthropicMessagesRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 128,
		Messages: []AnthropicInputMessage{
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_1","input":{"q":"llambo"}}]`)},
			{Role: "user", Content: json.RawMessage(`"continue"`)},
		},
	}
	if _, err := translateAnthropicRequest(reqMissingName); err == nil {
		t.Fatal("Expected error for tool_use missing name")
	}
}

func TestTranslateAnthropicRequest_RejectsToolResultMissingToolUseID(t *testing.T) {
	t.Parallel()
	req := AnthropicMessagesRequest{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 128,
		Messages: []AnthropicInputMessage{
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","content":"ok"}]`)},
		},
	}
	if _, err := translateAnthropicRequest(req); err == nil {
		t.Fatal("Expected error for tool_result missing tool_use_id")
	}
}

func TestBuildAnthropicResponse_IncludesToolUseBlocks(t *testing.T) {
	t.Parallel()
	res := buildAnthropicResponse(providers.ChatResult{
		Content:      "",
		FinishReason: "tool_calls",
		ToolCalls: []providers.ToolCall{
			{ID: "call_1", Name: "search", Arguments: `{"q":"llambo"}`},
		},
	}, nil)

	if res.StopReason != "tool_use" {
		t.Fatalf("Expected stop_reason tool_use, got %q", res.StopReason)
	}
	if len(res.Content) != 1 {
		t.Fatalf("Expected one tool_use content block, got %d", len(res.Content))
	}
	if res.Content[0].Type != "tool_use" || res.Content[0].ID != "call_1" || res.Content[0].Name != "search" {
		t.Fatalf("Unexpected tool_use block: %+v", res.Content[0])
	}
	if string(res.Content[0].Input) != `{"q":"llambo"}` {
		t.Fatalf("Unexpected tool input JSON: %s", string(res.Content[0].Input))
	}
}

func TestBuildAnthropicResponse_MixedTextAndToolUseBlocks(t *testing.T) {
	t.Parallel()
	res := buildAnthropicResponse(providers.ChatResult{
		Content:      "I should check that.",
		FinishReason: "tool_calls",
		ToolCalls: []providers.ToolCall{
			{ID: "call_2", Name: "search", Arguments: `{"query":"llambo"}`},
		},
	}, nil)

	if len(res.Content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(res.Content))
	}
	if res.Content[0].Type != "text" || res.Content[0].Text != "I should check that." {
		t.Fatalf("Unexpected first text block: %+v", res.Content[0])
	}
	if res.Content[1].Type != "tool_use" || res.Content[1].ID != "call_2" || res.Content[1].Name != "search" {
		t.Fatalf("Unexpected second tool_use block: %+v", res.Content[1])
	}
}

func TestToolInputRaw_InvalidJSONFallback(t *testing.T) {
	t.Parallel()
	raw := toolInputRaw("{invalid")
	if !strings.Contains(string(raw), "raw_arguments") {
		t.Fatalf("Expected fallback JSON wrapper, got %s", string(raw))
	}
}

func TestToolResultText_StringAndBlocks(t *testing.T) {
	t.Parallel()
	if got := toolResultText(json.RawMessage(`"ok"`)); got != "ok" {
		t.Fatalf("Expected plain string tool result, got %q", got)
	}

	got := toolResultText(json.RawMessage(`[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]`))
	if got != "line1\nline2" {
		t.Fatalf("Expected joined text block tool result, got %q", got)
	}
}

func TestBuildAnthropicResponse_TruncatesLongText(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", maxAnthropicResponseRunes+50)
	res := buildAnthropicResponse(providers.ChatResult{Content: long}, nil)
	if len(res.Content) != 1 || res.Content[0].Type != "text" {
		t.Fatalf("Expected single text block, got %+v", res.Content)
	}
	if len([]rune(res.Content[0].Text)) != maxAnthropicResponseRunes {
		t.Fatalf("Expected %d runes, got %d", maxAnthropicResponseRunes, len([]rune(res.Content[0].Text)))
	}
}

func TestBuildAnthropicResponse_CapsToolUseBlocks(t *testing.T) {
	t.Parallel()
	toolCalls := make([]providers.ToolCall, maxAnthropicToolUseBlocks+10)
	for i := range toolCalls {
		toolCalls[i] = providers.ToolCall{ID: "c", Name: "search", Arguments: `{"q":"x"}`}
	}
	res := buildAnthropicResponse(providers.ChatResult{ToolCalls: toolCalls, FinishReason: "tool_calls"}, nil)
	if len(res.Content) != maxAnthropicToolUseBlocks {
		t.Fatalf("Expected %d tool_use blocks, got %d", maxAnthropicToolUseBlocks, len(res.Content))
	}
}

func TestToolInputRaw_TruncatesVeryLargeArguments(t *testing.T) {
	t.Parallel()
	large := strings.Repeat("x", maxAnthropicToolInputBytes+10)
	raw := toolInputRaw(large)
	if !strings.Contains(string(raw), "truncated") {
		t.Fatalf("Expected truncated marker for large arguments, got %s", string(raw))
	}
}

func TestNormalizeStopSequences_DedupesAndDropsEmpty(t *testing.T) {
	t.Parallel()
	in := []string{"", "END", "STOP", "END", ""}
	out := normalizeStopSequences(in)
	if len(out) != 2 {
		t.Fatalf("Expected 2 normalized stop sequences, got %d", len(out))
	}
	if out[0] != "END" || out[1] != "STOP" {
		t.Fatalf("Unexpected normalized sequence order/content: %+v", out)
	}
}

func TestBuildAnthropicPrompts_PreservesRoleOrder(t *testing.T) {
	t.Parallel()
	system, convo := buildAnthropicPrompts([]Message{
		{Role: "system", Content: "Be concise."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
		{Role: "user", Content: "Question"},
	})

	if system != "Be concise." {
		t.Fatalf("Unexpected system prompt: %q", system)
	}
	wantConvo := "User: Hello\nAssistant: Hi\nUser: Question"
	if convo != wantConvo {
		t.Fatalf("Unexpected conversation prompt: got %q want %q", convo, wantConvo)
	}
}

func TestParseAnthropicTooling_ValidAndChoice(t *testing.T) {
	t.Parallel()
	tools, choice, err := parseAnthropicTooling(
		[]AnthropicTool{{Name: "search", Description: "search docs", InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)}},
		json.RawMessage(`{"type":"tool","name":"search"}`),
	)
	if err != nil {
		t.Fatalf("Expected valid tooling parse, got error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Fatalf("Unexpected tools parse result: %+v", tools)
	}
	if choice == nil || choice.Type != "tool" || choice.Name != "search" {
		t.Fatalf("Unexpected tool choice parse result: %+v", choice)
	}
}

func TestParseAnthropicTooling_ChoiceWithoutToolsRejected(t *testing.T) {
	t.Parallel()
	_, _, err := parseAnthropicTooling(nil, json.RawMessage(`{"type":"any"}`))
	if err == nil {
		t.Fatal("Expected error when tool_choice is set without tools")
	}
}

func TestParseAnthropicTooling_ChoiceToolMustExist(t *testing.T) {
	t.Parallel()
	_, _, err := parseAnthropicTooling(
		[]AnthropicTool{{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		json.RawMessage(`{"type":"tool","name":"lookup"}`),
	)
	if err == nil {
		t.Fatal("Expected error when tool_choice name is missing from tools")
	}
}

func TestValidateAnthropicOptionalFields(t *testing.T) {
	t.Parallel()
	topKInvalid := 0
	if err := validateAnthropicOptionalFields(AnthropicMessagesRequest{TopK: &topKInvalid}); err == nil {
		t.Fatal("Expected top_k validation error")
	}

	topPInvalid := 1.1
	if err := validateAnthropicOptionalFields(AnthropicMessagesRequest{TopP: &topPInvalid}); err == nil {
		t.Fatal("Expected top_p validation error")
	}

	tempInvalid := -0.1
	if err := validateAnthropicOptionalFields(AnthropicMessagesRequest{Temperature: &tempInvalid}); err == nil {
		t.Fatal("Expected temperature validation error")
	}

	if err := validateAnthropicOptionalFields(AnthropicMessagesRequest{Thinking: json.RawMessage(`"bad"`)}); err == nil {
		t.Fatal("Expected thinking validation error for non-object")
	}

	topK := 10
	topP := 0.9
	temp := 0.2
	if err := validateAnthropicOptionalFields(AnthropicMessagesRequest{TopK: &topK, TopP: &topP, Temperature: &temp, Thinking: json.RawMessage(`{"type":"enabled"}`)}); err != nil {
		t.Fatalf("Expected valid optional fields, got error: %v", err)
	}
}

func TestNormalizeAnthropicMetadata(t *testing.T) {
	t.Parallel()
	meta := normalizeAnthropicMetadata(map[string]any{
		"trace_id": "abc",
		"count":    12,
	})
	if meta["trace_id"] != "abc" {
		t.Fatalf("Expected trace_id metadata, got %+v", meta)
	}
	if meta["count"] != "12" {
		t.Fatalf("Expected JSON stringified numeric metadata, got %+v", meta)
	}
}

func TestNormalizeAnthropicMetadata_LimitsAndTruncates(t *testing.T) {
	t.Parallel()
	metaIn := make(map[string]any)
	for i := 0; i < maxAnthropicMetadataPairs+5; i++ {
		metaIn[fmt.Sprintf("k_%02d_%s", i, strings.Repeat("x", 80))] = strings.Repeat("v", 700)
	}

	out := normalizeAnthropicMetadata(metaIn)
	if len(out) != maxAnthropicMetadataPairs {
		t.Fatalf("Expected %d metadata pairs, got %d", maxAnthropicMetadataPairs, len(out))
	}
	for k, v := range out {
		if len([]rune(k)) > maxAnthropicMetadataKeyLen {
			t.Fatalf("Metadata key exceeds max length: %q", k)
		}
		if len([]rune(v)) > maxAnthropicMetadataValLen {
			t.Fatalf("Metadata value exceeds max length: %q", v)
		}
	}
}

func TestAnthropicJSONOverrides(t *testing.T) {
	t.Parallel()
	topK := 42
	req := AnthropicMessagesRequest{
		TopK:     &topK,
		Thinking: json.RawMessage(`{"type":"enabled","budget":1024}`),
	}
	o := anthropicJSONOverrides(req)
	if o == nil {
		t.Fatal("Expected non-nil JSON overrides")
	}
	if o["top_k"] != 42 {
		t.Fatalf("Expected top_k override 42, got %+v", o["top_k"])
	}
	if _, ok := o["thinking"]; !ok {
		t.Fatalf("Expected thinking override, got %+v", o)
	}
}
