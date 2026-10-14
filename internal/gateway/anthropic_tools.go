package gateway

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dotcommander/llambo/providers"
)

// Repeated string literals consolidated by goconst.
const (
	anyStr = "any"
	auto1  = "auto"
	tool2  = "tool"
)

const none1 = "none"

func parseAnthropicTooling(tools []AnthropicTool, rawChoice json.RawMessage) ([]providers.ToolDefinition, *providers.ToolChoice, error) {
	if len(tools) > maxAnthropicTools {
		return nil, nil, fmt.Errorf("tools exceeds maximum of %d", maxAnthropicTools)
	}
	parsedTools := make([]providers.ToolDefinition, 0, len(tools))
	for i, tool := range tools {
		if tool.Name == "" {
			return nil, nil, fmt.Errorf("tools[%d].name is required", i)
		}
		if len(tool.Name) > 64 {
			return nil, nil, fmt.Errorf("tools[%d].name exceeds maximum length of 64", i)
		}
		if !isRawNull(tool.InputSchema) && !json.Valid(tool.InputSchema) {
			return nil, nil, fmt.Errorf("tools[%d].input_schema must be valid JSON", i)
		}
		schema := tool.InputSchema
		if isRawNull(schema) {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		parsedTools = append(parsedTools, providers.ToolDefinition{
			Name: tool.Name, Description: tool.Description, InputSchema: schema,
		})
	}
	choice, err := parseAnthropicToolChoice(rawChoice)
	if err != nil {
		return nil, nil, err
	}
	if err := validateAnthropicToolSelection(parsedTools, choice); err != nil {
		return nil, nil, err
	}
	return parsedTools, choice, nil
}

func validateAnthropicToolSelection(tools []providers.ToolDefinition, choice *providers.ToolChoice) error {
	if choice != nil && len(tools) == 0 {
		return errors.New("tool_choice requires tools to be defined")
	}
	if choice != nil && choice.Type == tool2 {
		for _, tool := range tools {
			if tool.Name == choice.Name {
				return nil
			}
		}
		return fmt.Errorf("tool_choice.name %q is not present in tools", choice.Name)
	}
	return nil
}

func parseAnthropicToolChoice(raw json.RawMessage) (*providers.ToolChoice, error) {
	if isRawNull(raw) {
		return nil, nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		switch asString {
		case auto1, none1, anyStr:
			return &providers.ToolChoice{Type: asString}, nil
		default:
			return nil, fmt.Errorf("tool_choice string %q is not supported", asString)
		}
	}
	var payload struct {
		Type string `json:"type"`
		Name string `json:"name,omitempty"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("tool_choice must be a string or object")
	}
	switch payload.Type {
	case auto1, none1, anyStr:
		return &providers.ToolChoice{Type: payload.Type}, nil
	case tool2:
		if payload.Name == "" {
			return nil, errors.New("tool_choice.name is required when type is tool")
		}
		return &providers.ToolChoice{Type: tool2, Name: payload.Name}, nil
	default:
		return nil, fmt.Errorf("tool_choice.type %q is not supported", payload.Type)
	}
}
