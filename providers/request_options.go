package providers

import (
	"context"
	"encoding/json"
)

type requestOptionsKey string

const (
	stopSequencesContextKey  requestOptionsKey = "stop_sequences"
	toolsContextKey          requestOptionsKey = "tools"
	toolChoiceContextKey     requestOptionsKey = "tool_choice"
	chatOverridesContextKey  requestOptionsKey = "chat_overrides"
	metadataContextKey       requestOptionsKey = "metadata"
	jsonOverridesContextKey  requestOptionsKey = "json_overrides"
	responseFormatContextKey requestOptionsKey = "response_format"
)

// ToolDefinition captures tool metadata for provider-native tool calling.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// WithResponseFormatOverride attaches an OpenAI-compatible response_format
// value to a request. The value is cloned so callers cannot alter an in-flight
// request after it is attached to the context.
func WithResponseFormatOverride(ctx context.Context, responseFormat any) context.Context {
	if ctx == nil || responseFormat == nil {
		return ctx
	}
	return context.WithValue(ctx, responseFormatContextKey, cloneJSONValue(responseFormat))
}

// ResponseFormatOverrideFromContext extracts a cloned response_format value.
func ResponseFormatOverrideFromContext(ctx context.Context) any {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(responseFormatContextKey)
	if v == nil {
		return nil
	}
	return cloneJSONValue(v)
}

// ToolChoice describes preferred tool call behavior.
type ToolChoice struct {
	Type string
	Name string
}

// ChatRequestOverrides carries per-request parameter overrides.
type ChatRequestOverrides struct {
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
}

// WithStopSequences attaches stop sequences to request context.
func WithStopSequences(ctx context.Context, sequences []string) context.Context {
	if ctx == nil || len(sequences) == 0 {
		return ctx
	}
	copySeq := make([]string, len(sequences))
	copy(copySeq, sequences)
	return context.WithValue(ctx, stopSequencesContextKey, copySeq)
}

// StopSequencesFromContext extracts stop sequences from request context.
func StopSequencesFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(stopSequencesContextKey)
	seq, ok := v.([]string)
	if !ok || len(seq) == 0 {
		return nil
	}
	out := make([]string, len(seq))
	copy(out, seq)
	return out
}

// WithTools attaches tool definitions and optional tool choice to context.
func WithTools(ctx context.Context, tools []ToolDefinition, choice *ToolChoice) context.Context {
	if ctx == nil {
		return ctx
	}
	if len(tools) > 0 {
		copyTools := make([]ToolDefinition, len(tools))
		copy(copyTools, tools)
		ctx = context.WithValue(ctx, toolsContextKey, copyTools)
	}
	if choice != nil {
		copyChoice := *choice
		ctx = context.WithValue(ctx, toolChoiceContextKey, &copyChoice)
	}
	return ctx
}

// ToolsFromContext extracts tool definitions from context.
func ToolsFromContext(ctx context.Context) []ToolDefinition {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(toolsContextKey)
	tools, ok := v.([]ToolDefinition)
	if !ok || len(tools) == 0 {
		return nil
	}
	out := make([]ToolDefinition, len(tools))
	copy(out, tools)
	return out
}

// ToolChoiceFromContext extracts tool choice from context.
func ToolChoiceFromContext(ctx context.Context) *ToolChoice {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(toolChoiceContextKey)
	choice, ok := v.(*ToolChoice)
	if !ok || choice == nil {
		return nil
	}
	out := *choice
	return &out
}

// WithChatRequestOverrides attaches per-request parameter overrides to context.
func WithChatRequestOverrides(ctx context.Context, maxTokens *int, temperature *float64, topP *float64) context.Context {
	if ctx == nil {
		return ctx
	}
	if maxTokens == nil && temperature == nil && topP == nil {
		return ctx
	}

	o := ChatRequestOverrides{}
	if maxTokens != nil {
		v := *maxTokens
		o.MaxTokens = &v
	}
	if temperature != nil {
		v := *temperature
		o.Temperature = &v
	}
	if topP != nil {
		v := *topP
		o.TopP = &v
	}

	return context.WithValue(ctx, chatOverridesContextKey, &o)
}

// ChatRequestOverridesFromContext extracts per-request parameter overrides.
func ChatRequestOverridesFromContext(ctx context.Context) *ChatRequestOverrides {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(chatOverridesContextKey)
	o, ok := v.(*ChatRequestOverrides)
	if !ok || o == nil {
		return nil
	}
	out := *o
	return &out
}

// WithRequestMetadata attaches metadata to request context.
func WithRequestMetadata(ctx context.Context, metadata map[string]string) context.Context {
	if ctx == nil || len(metadata) == 0 {
		return ctx
	}
	out := make(map[string]string, len(metadata))
	for k, v := range metadata {
		out[k] = v
	}
	return context.WithValue(ctx, metadataContextKey, out)
}

// RequestMetadataFromContext extracts request metadata from context.
func RequestMetadataFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(metadataContextKey)
	meta, ok := v.(map[string]string)
	if !ok || len(meta) == 0 {
		return nil
	}
	out := make(map[string]string, len(meta))
	for k, val := range meta {
		out[k] = val
	}
	return out
}

// WithJSONOverrides attaches raw top-level JSON body overrides to context.
func WithJSONOverrides(ctx context.Context, overrides map[string]any) context.Context {
	if ctx == nil || len(overrides) == 0 {
		return ctx
	}
	out := make(map[string]any, len(overrides))
	for k, v := range overrides {
		out[k] = v
	}
	return context.WithValue(ctx, jsonOverridesContextKey, out)
}

// JSONOverridesFromContext extracts raw JSON overrides from context.
func JSONOverridesFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(jsonOverridesContextKey)
	o, ok := v.(map[string]any)
	if !ok || len(o) == 0 {
		return nil
	}
	return cloneJSONMap(o)
}

func cloneJSONMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneJSONValue(v)
	}
	return out
}

func cloneJSONValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		return cloneJSONMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneJSONValue(item)
		}
		return out
	case json.RawMessage:
		return append(json.RawMessage(nil), v...)
	default:
		return v
	}
}
