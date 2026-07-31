package gateway

import (
	"context"
	"encoding/json"

	"github.com/dotcommander/llambo/providers"
)

const anthropicPrefillContinuationPrompt = "Continue the assistant response from where it ended. Do not repeat prior text."

const (
	maxAnthropicMessages       = 10000
	maxAnthropicContentBlocks  = 5000
	maxAnthropicStopSequences  = 256
	maxAnthropicTools          = 128
	maxAnthropicMetadataPairs  = 16
	maxAnthropicMetadataKeyLen = 64
	maxAnthropicMetadataValLen = 512
	maxAnthropicResponseRunes  = 100000
	maxAnthropicToolUseBlocks  = 128
	maxAnthropicToolInputBytes = 65536
)

// isRawNull reports whether a json.RawMessage is empty or the JSON literal null.
func isRawNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

// AnthropicMessagesRequest is the request shape for POST /v1/messages.
type AnthropicMessagesRequest struct {
	Model         string                  `json:"model"`
	Messages      []AnthropicInputMessage `json:"messages"`
	System        json.RawMessage         `json:"system,omitempty"`
	MaxTokens     int                     `json:"max_tokens"`
	Temperature   *float64                `json:"temperature,omitempty"`
	TopP          *float64                `json:"top_p,omitempty"`
	TopK          *int                    `json:"top_k,omitempty"`
	Metadata      map[string]any          `json:"metadata,omitempty"`
	Thinking      json.RawMessage         `json:"thinking,omitempty"`
	Stream        bool                    `json:"stream,omitempty"`
	StopSequences []string                `json:"stop_sequences,omitempty"`
	Tools         []AnthropicTool         `json:"tools,omitempty"`
	ToolChoice    json.RawMessage         `json:"tool_choice,omitempty"`
}

// AnthropicTool is a tool definition in request payload.
type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// AnthropicInputMessage is one input conversation message.
type AnthropicInputMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// AnthropicContentBlock represents one input/response content block.
type AnthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	Source    json.RawMessage `json:"source,omitempty"`
}

// AnthropicMessagesResponse is the response shape for POST /v1/messages.
type AnthropicMessagesResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence"`
	Usage        AnthropicUsage          `json:"usage"`
}

// AnthropicUsage mirrors Anthropic token usage fields.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicErrorResponse mirrors Anthropic error envelope.
type AnthropicErrorResponse struct {
	Type      string               `json:"type"`
	Error     AnthropicErrorDetail `json:"error"`
	RequestID string               `json:"request_id"`
}

// AnthropicErrorDetail contains error type and message.
type AnthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// anthropicParsedRequest bundles all parsed fields from handleAnthropicMessages.
type anthropicParsedRequest struct {
	RequestID     string
	Original      AnthropicMessagesRequest
	Translated    ChatCompletionRequest
	Tools         []providers.ToolDefinition
	ToolChoice    *providers.ToolChoice
	Metadata      map[string]string
	JSONOverrides map[string]any
}

func (p *anthropicParsedRequest) enrichContext(ctx context.Context) context.Context {
	ctx = providers.WithStopSequences(ctx, p.Original.StopSequences)
	ctx = providers.WithTools(ctx, p.Tools, p.ToolChoice)
	ctx = providers.WithRequestMetadata(ctx, p.Metadata)
	ctx = providers.WithJSONOverrides(ctx, p.JSONOverrides)
	ctx = providers.WithChatRequestOverrides(ctx, p.Translated.MaxTokens, p.Translated.Temperature, p.Translated.TopP)
	return ctx
}
