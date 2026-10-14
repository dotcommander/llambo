package gateway

import (
	"errors"
	"fmt"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func structuredMessage(m Message, i int) (whtypes.Message, error) {
	switch m.Role {
	case system3:
		return whtypes.NewSystemMessage(m.Content), nil
	case user4:
		return whtypes.NewUserMessage(m.Content), nil
	case assistant1:
		calls, err := structuredAssistantToolCalls(m.ToolCalls, i)
		if err != nil {
			return nil, err
		}
		return &whtypes.AssistantMessage{Content: m.Content, ToolCalls: calls}, nil
	case tool2:
		if m.ToolCallID == "" {
			return nil, fmt.Errorf("messages[%d].tool_call_id is required", i)
		}
		return &whtypes.ToolResultMessage{Content: m.Content, ToolCallID: m.ToolCallID}, nil
	default:
		return nil, fmt.Errorf("messages[%d].role is unsupported", i)
	}
}

func structuredMessages(messages []Message) ([]whtypes.Message, error) {
	var out []whtypes.Message
	for i, m := range messages {
		message, err := structuredMessage(m, i)
		if err != nil {
			return out, err
		}
		out = append(out, message)
		if m.Role != assistant1 && len(m.ToolCalls) > 0 {
			return out, errors.New("tool_calls requires assistant role")
		}
	}
	return out, nil
}

func structuredAssistantToolCalls(toolCalls []whtypes.ToolCall, i int) ([]whtypes.ToolCall, error) {
	calls := make([]whtypes.ToolCall, 0, len(toolCalls))
	for _, call := range toolCalls {
		if call.ID == "" || (call.Name == "" && (call.Function == nil || call.Function.Name == "")) || (call.Type != "" && call.Type != function1) {
			return nil, fmt.Errorf("messages[%d]: invalid tool call", i)
		}
		normalized, err := whtypes.NormalizeToolCall(call)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		calls = append(calls, normalized)
	}
	return calls, nil
}

func validateStructuredTool(tool whtypes.Tool) error {
	if tool.Type != "" && tool.Type != function1 {
		return errors.New("unsupported tool type")
	}
	if tool.Name == "" && (tool.Function == nil || tool.Function.Name == "") {
		return errors.New("tool name is required")
	}
	return nil
}
