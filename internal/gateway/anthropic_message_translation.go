package gateway

import (
	"encoding/json"
	"errors"
	"fmt"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

type anthropicMessageTranslation struct {
	role    string
	out     []Message
	current Message
}

func (state *anthropicMessageTranslation) flush() {
	if state.current.Content != "" || len(state.current.ToolCalls) > 0 {
		state.out = append(state.out, state.current)
		state.current = Message{Role: state.role}
	}
}

func (state *anthropicMessageTranslation) appendBlock(b AnthropicContentBlock) error {
	switch b.Type {
	case text1:
		state.current.Content += b.Text
		return nil
	case toolUse:
		return state.appendToolUse(b)
	case "tool_result":
		return state.appendToolResult(b)
	default:
		return fmt.Errorf("unsupported content block %q", b.Type)
	}
}

func (state *anthropicMessageTranslation) appendToolUse(b AnthropicContentBlock) error {
	if state.role != assistant1 || b.ID == "" || b.Name == "" {
		return errors.New("invalid tool_use block")
	}
	var args map[string]any
	if json.Unmarshal(b.Input, &args) != nil || args == nil {
		return errors.New("tool_use.input must be an object")
	}
	state.current.ToolCalls = append(state.current.ToolCalls, whtypes.ToolCall{Type: function1, ID: b.ID, Name: b.Name, Arguments: args, Function: &whtypes.ToolCallFunction{Name: b.Name, Arguments: string(b.Input)}})
	return nil
}

func (state *anthropicMessageTranslation) appendToolResult(b AnthropicContentBlock) error {
	if state.role != user4 || b.ToolUseID == "" {
		return errors.New("invalid tool_result block")
	}
	state.flush()
	content, err := parseAnthropicContent(b.Content, "tool_result.content")
	if err != nil {
		var value map[string]any
		if json.Unmarshal(b.Content, &value) != nil {
			return err
		}
		content = compactJSON(b.Content)
	}
	state.out = append(state.out, Message{Role: tool2, ToolCallID: b.ToolUseID, Content: content})
	return nil
}
