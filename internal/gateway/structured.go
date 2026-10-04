package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dotcommander/llambo/providers"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
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
	req.target = target
	req.structured = request
	return nil
}
func structuredRequest(req ChatCompletionRequest) (providers.StructuredChatRequest, error) {
	out := providers.StructuredChatRequest{Tools: req.Tools, ResponseFormat: responseFormatOverride(req.ResponseFormat), Temperature: req.Temperature, TopP: req.TopP, MaxTokens: req.MaxTokens}
	if len(req.Messages) == 0 {
		return out, fmt.Errorf("messages is required")
	}
	for i, m := range req.Messages {
		switch m.Role {
		case "system":
			out.Messages = append(out.Messages, whtypes.NewSystemMessage(m.Content))
		case "user":
			out.Messages = append(out.Messages, whtypes.NewUserMessage(m.Content))
		case "assistant":
			calls := make([]whtypes.ToolCall, 0, len(m.ToolCalls))
			for _, call := range m.ToolCalls {
				if call.ID == "" || (call.Name == "" && (call.Function == nil || call.Function.Name == "")) || (call.Type != "" && call.Type != "function") {
					return out, fmt.Errorf("messages[%d]: invalid tool call", i)
				}
				normalized, err := whtypes.NormalizeToolCall(call)
				if err != nil {
					return out, fmt.Errorf("messages[%d]: %w", i, err)
				}
				calls = append(calls, normalized)
			}
			out.Messages = append(out.Messages, &whtypes.AssistantMessage{Content: m.Content, ToolCalls: calls})
		case "tool":
			if m.ToolCallID == "" {
				return out, fmt.Errorf("messages[%d].tool_call_id is required", i)
			}
			out.Messages = append(out.Messages, &whtypes.ToolResultMessage{Content: m.Content, ToolCallID: m.ToolCallID})
		default:
			return out, fmt.Errorf("messages[%d].role is unsupported", i)
		}
		if m.Role != "assistant" && len(m.ToolCalls) > 0 {
			return out, fmt.Errorf("tool_calls requires assistant role")
		}
	}
	for _, tool := range req.Tools {
		if tool.Type != "" && tool.Type != "function" {
			return out, fmt.Errorf("unsupported tool type")
		}
		if tool.Name == "" && (tool.Function == nil || tool.Function.Name == "") {
			return out, fmt.Errorf("tool name is required")
		}
	}
	if !isRawNull(req.Stop) {
		var single string
		if json.Unmarshal(req.Stop, &single) == nil {
			out.Stop = []string{single}
		} else if err := json.Unmarshal(req.Stop, &out.Stop); err != nil {
			return out, fmt.Errorf("stop must be a string or string array")
		}
	}
	if !isRawNull(req.ToolChoice) {
		var choice string
		if json.Unmarshal(req.ToolChoice, &choice) == nil {
			switch choice {
			case "auto", "none":
				out.ToolChoice = &whtypes.ToolChoice{Type: whtypes.ToolChoiceType(choice)}
			case "required", "any":
				out.ToolChoice = &whtypes.ToolChoice{Type: whtypes.ToolChoiceTypeAny}
			default:
				return out, fmt.Errorf("unsupported tool_choice")
			}
		} else {
			var choice struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if json.Unmarshal(req.ToolChoice, &choice) != nil || choice.Type != "function" || choice.Function.Name == "" {
				return out, fmt.Errorf("unsupported tool_choice")
			}
			out.ToolChoice = &whtypes.ToolChoice{Type: whtypes.ToolChoiceTypeSpecific, ToolName: choice.Function.Name}
		}
	}
	if req.ResponseFormat != nil {
		switch strings.ToLower(req.ResponseFormat.Type) {
		case "text", "json_object", "json_schema":
		default:
			return out, fmt.Errorf("unsupported response_format")
		}
	}
	return out, nil
}
func (s *Server) executeChat(ctx context.Context, req ChatCompletionRequest) (providers.ChatResult, error) {
	if p, ok := s.provider.(structuredChatProvider); ok {
		return p.ChatStructuredWithInfoContext(ctx, req.structured, req.target)
	}
	if req.Model != "" || len(req.Tools) > 0 {
		return providers.ChatResult{}, fmt.Errorf("structured requests unavailable")
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
		return providers.ChatResult{}, fmt.Errorf("streaming unavailable")
	}
	if req.Model != "" || len(req.Tools) > 0 {
		return providers.ChatResult{}, fmt.Errorf("structured requests unavailable")
	}
	system, user := ExtractPrompts(req.Messages)
	return p.ChatStreamWithInfoContext(ctx, system, user, callback)
}

func openAIResultMessage(result providers.ChatResult) *Message {
	message := &Message{Role: "assistant", Content: result.Content}
	for _, call := range result.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, whtypes.ToolCall{ID: call.ID, Type: "function", Function: &whtypes.ToolCallFunction{Name: call.Name, Arguments: call.Arguments}})
	}
	return message
}

func (r JobRequest) chatRequest() ChatCompletionRequest {
	return ChatCompletionRequest{Messages: r.Messages, Tools: r.Tools, ToolChoice: r.ToolChoice, Stop: r.Stop, ResponseFormat: r.ResponseFormat, Temperature: r.Temperature, TopP: r.TopP, MaxTokens: r.MaxTokens}
}

func openAIResultToolCalls(calls []providers.ToolCall) []OpenAIToolCall {
	out := make([]OpenAIToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, OpenAIToolCall{ID: call.ID, Type: "function", Function: &whtypes.ToolCallFunction{Name: call.Name, Arguments: call.Arguments}})
	}
	return out
}
