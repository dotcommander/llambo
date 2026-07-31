package gateway

import (
	"encoding/json"
	"fmt"

	"github.com/dotcommander/llambo/providers"
)

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
	if choice != nil && len(parsedTools) == 0 {
		return nil, nil, fmt.Errorf("tool_choice requires tools to be defined")
	}
	if choice != nil && choice.Type == "tool" {
		for _, tool := range parsedTools {
			if tool.Name == choice.Name {
				return parsedTools, choice, nil
			}
		}
		return nil, nil, fmt.Errorf("tool_choice.name %q is not present in tools", choice.Name)
	}
	return parsedTools, choice, nil
}

func parseAnthropicToolChoice(raw json.RawMessage) (*providers.ToolChoice, error) {
	if isRawNull(raw) {
		return nil, nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		switch asString {
		case "auto", "none", "any":
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
		return nil, fmt.Errorf("tool_choice must be a string or object")
	}
	switch payload.Type {
	case "auto", "none", "any":
		return &providers.ToolChoice{Type: payload.Type}, nil
	case "tool":
		if payload.Name == "" {
			return nil, fmt.Errorf("tool_choice.name is required when type is tool")
		}
		return &providers.ToolChoice{Type: "tool", Name: payload.Name}, nil
	default:
		return nil, fmt.Errorf("tool_choice.type %q is not supported", payload.Type)
	}
}
