package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dotcommander/llambo/providers"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

// Repeated string literals consolidated by goconst.
const (
	assistant1 = "assistant"
	function1  = "function"
)

type structuredChatProvider interface {
	ChatStructuredWithInfoContext(context.Context, providers.StructuredChatRequest, providers.ResolvedTarget) (providers.ChatResult, error)
}
type structuredStreamProvider interface {
	ChatStructuredStreamWithInfoContext(context.Context, providers.StructuredChatRequest, providers.ResolvedTarget, providers.ChatStreamHandler) (providers.ChatResult, error)
}

func (s *Server) prepareChat(req *ChatCompletionRequest) error {
	target, err := providers.ResolveTarget(s.configs, req.Model, "")
	if err != nil {
		return err
	}
	request, err := structuredRequest(*req)
	if err != nil {
		return err
	}
	reasoning, err := reasoningOverride(*req)
	if err != nil {
		return err
	}
	req.target = target
	req.structured = request
	req.reasoning = reasoning
	return nil
}
func structuredRequest(req ChatCompletionRequest) (providers.StructuredChatRequest, error) {
	out := providers.StructuredChatRequest{Tools: req.Tools, ResponseFormat: responseFormatOverride(req.ResponseFormat), Temperature: req.Temperature, TopP: req.TopP, MaxTokens: req.MaxTokens}
	if len(req.Messages) == 0 {
		return out, errors.New("messages is required")
	}
	for i, m := range req.Messages {
		switch m.Role {
		case system3:
			out.Messages = append(out.Messages, whtypes.NewSystemMessage(m.Content))
		case user4:
			out.Messages = append(out.Messages, whtypes.NewUserMessage(m.Content))
		case assistant1:
			calls := make([]whtypes.ToolCall, 0, len(m.ToolCalls))
			for _, call := range m.ToolCalls {
				if call.ID == "" || (call.Name == "" && (call.Function == nil || call.Function.Name == "")) || (call.Type != "" && call.Type != function1) {
					return out, fmt.Errorf("messages[%d]: invalid tool call", i)
				}
				normalized, err := whtypes.NormalizeToolCall(call)
				if err != nil {
					return out, fmt.Errorf("messages[%d]: %w", i, err)
				}
				calls = append(calls, normalized)
			}
			out.Messages = append(out.Messages, &whtypes.AssistantMessage{Content: m.Content, ToolCalls: calls})
		case tool2:
			if m.ToolCallID == "" {
				return out, fmt.Errorf("messages[%d].tool_call_id is required", i)
			}
			out.Messages = append(out.Messages, &whtypes.ToolResultMessage{Content: m.Content, ToolCallID: m.ToolCallID})
		default:
			return out, fmt.Errorf("messages[%d].role is unsupported", i)
		}
		if m.Role != assistant1 && len(m.ToolCalls) > 0 {
			return out, errors.New("tool_calls requires assistant role")
		}
	}
	for _, tool := range req.Tools {
		if tool.Type != "" && tool.Type != function1 {
			return out, errors.New("unsupported tool type")
		}
		if tool.Name == "" && (tool.Function == nil || tool.Function.Name == "") {
			return out, errors.New("tool name is required")
		}
	}
	if !isRawNull(req.Stop) {
		var single string
		if json.Unmarshal(req.Stop, &single) == nil {
			out.Stop = []string{single}
		} else if err := json.Unmarshal(req.Stop, &out.Stop); err != nil {
			return out, errors.New("stop must be a string or string array")
		}
	}
	if !isRawNull(req.ToolChoice) {
		choice, err := parseToolChoice(req.ToolChoice)
		if err != nil {
			return out, err
		}
		out.ToolChoice = choice
	}
	if req.ResponseFormat != nil {
		switch strings.ToLower(req.ResponseFormat.Type) {
		case text1, "json_object", "json_schema":
		default:
			return out, errors.New("unsupported response_format")
		}
	}
	return out, nil
}
func (s *Server) executeChat(ctx context.Context, req ChatCompletionRequest) (providers.ChatResult, error) {
	if p, ok := s.provider.(structuredChatProvider); ok {
		return p.ChatStructuredWithInfoContext(ctx, req.structured, req.target)
	}
	if req.Model != "" || len(req.Tools) > 0 {
		return providers.ChatResult{}, errors.New("structured requests unavailable")
	}
	system, user := ExtractPrompts(req.Messages)
	return s.provider.ChatWithInfoContext(ctx, system, user)
}
func (s *Server) executeChatStream(ctx context.Context, req ChatCompletionRequest, callback providers.ChatStreamHandler) (providers.ChatResult, error) {
	if p, ok := s.provider.(structuredStreamProvider); ok {
		return p.ChatStructuredStreamWithInfoContext(ctx, req.structured, req.target, callback)
	}
	p, ok := s.provider.(providers.ChatStreamProvider)
	if !ok {
		return providers.ChatResult{}, errors.New("streaming unavailable")
	}
	if req.Model != "" || len(req.Tools) > 0 {
		return providers.ChatResult{}, errors.New("structured requests unavailable")
	}
	system, user := ExtractPrompts(req.Messages)
	return p.ChatStreamWithInfoContext(ctx, system, user, callback)
}

func openAIResultMessage(result providers.ChatResult) *Message {
	message := &Message{Role: assistant1, Content: result.Content}
	for _, call := range result.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, whtypes.ToolCall{ID: call.ID, Type: function1, Function: &whtypes.ToolCallFunction{Name: call.Name, Arguments: call.Arguments}})
	}
	return message
}

func (r JobRequest) chatRequest() ChatCompletionRequest {
	return ChatCompletionRequest{Messages: r.Messages, Tools: r.Tools, ToolChoice: r.ToolChoice, Stop: r.Stop, ResponseFormat: r.ResponseFormat, Temperature: r.Temperature, TopP: r.TopP, MaxTokens: r.MaxTokens}
}

func openAIResultToolCalls(calls []providers.ToolCall) []OpenAIToolCall {
	out := make([]OpenAIToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, OpenAIToolCall{ID: call.ID, Type: function1, Function: &whtypes.ToolCallFunction{Name: call.Name, Arguments: call.Arguments}})
	}
	return out
}

// parseToolChoice accepts the OpenAI string form (auto/none/required) plus the
// Anthropic-compatible "any" alias and the named {type:function} object form.
func parseToolChoice(raw json.RawMessage) (*whtypes.ToolChoice, error) {
	var choice string
	if json.Unmarshal(raw, &choice) == nil {
		switch choice {
		case auto1, none1:
			return &whtypes.ToolChoice{Type: whtypes.ToolChoiceType(choice)}, nil
		case "required", anyStr:
			return &whtypes.ToolChoice{Type: whtypes.ToolChoiceTypeAny}, nil
		default:
			return nil, errors.New("unsupported tool_choice")
		}
	}
	var named struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &named) != nil || named.Type != function1 || named.Function.Name == "" {
		return nil, errors.New("unsupported tool_choice")
	}
	return &whtypes.ToolChoice{Type: whtypes.ToolChoiceTypeSpecific, ToolName: named.Function.Name}, nil
}
