package gateway

import (
	"encoding/json"
	"fmt"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

// The HTTP representation contains OpenAI function objects; Wormhole's neutral
// fields stay internal to the ordered executor contract.
func (m Message) MarshalJSON() ([]byte, error) {
	calls := make([]OpenAIToolCall, 0, len(m.ToolCalls))
	for _, call := range m.ToolCalls {
		function := call.Function
		if function == nil {
			arguments, err := json.Marshal(call.Arguments)
			if err != nil {
				return nil, err
			}
			function = &whtypes.ToolCallFunction{Name: call.Name, Arguments: string(arguments)}
		}
		calls = append(calls, OpenAIToolCall{ID: call.ID, Type: "function", Function: function})
	}
	return json.Marshal(struct {
		Role       string           `json:"role"`
		Content    string           `json:"content"`
		ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
		ToolCallID string           `json:"tool_call_id,omitempty"`
	}{m.Role, m.Content, calls, m.ToolCallID})
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role       string             `json:"role"`
		Content    json.RawMessage    `json:"content"`
		ToolCalls  []whtypes.ToolCall `json:"tool_calls"`
		ToolCallID string             `json:"tool_call_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	content := ""
	if isRawNull(raw.Content) {
		if raw.Role != "assistant" || len(raw.ToolCalls) == 0 {
			return fmt.Errorf("message content must be a string")
		}
	} else if err := json.Unmarshal(raw.Content, &content); err != nil {
		return fmt.Errorf("message content must be a string")
	}
	*m = Message{Role: raw.Role, Content: content, ToolCalls: raw.ToolCalls, ToolCallID: raw.ToolCallID}
	return nil
}
