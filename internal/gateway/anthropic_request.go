package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func translateAnthropicRequest(req AnthropicMessagesRequest) (ChatCompletionRequest, error) {
	if len(req.Messages) == 0 {
		return ChatCompletionRequest{}, fmt.Errorf("messages array is required")
	}
	if len(req.Messages) > maxAnthropicMessages {
		return ChatCompletionRequest{}, fmt.Errorf("messages exceeds maximum of %d", maxAnthropicMessages)
	}
	if req.MaxTokens <= 0 {
		return ChatCompletionRequest{}, fmt.Errorf("max_tokens is required and must be > 0")
	}

	messages := make([]Message, 0, len(req.Messages)+1)

	system, err := parseAnthropicSystem(req.System)
	if err != nil {
		return ChatCompletionRequest{}, err
	}
	if system != "" {
		messages = append(messages, Message{Role: "system", Content: system})
	}

	for i, msg := range req.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			return ChatCompletionRequest{}, fmt.Errorf("messages[%d].role must be user or assistant", i)
		}

		content, err := parseAnthropicContent(msg.Content, fmt.Sprintf("messages[%d].content", i))
		if err != nil {
			return ChatCompletionRequest{}, err
		}

		messages = append(messages, Message{Role: msg.Role, Content: content})
	}

	if req.Messages[len(req.Messages)-1].Role == "assistant" {
		messages = append(messages, Message{Role: "user", Content: anthropicPrefillContinuationPrompt})
	}

	maxTokens := req.MaxTokens
	return ChatCompletionRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   &maxTokens,
		Stream:      req.Stream,
	}, nil
}

func parseAnthropicSystem(raw json.RawMessage) (string, error) {
	if isRawNull(raw) {
		return "", nil
	}
	return parseAnthropicContent(raw, "system")
}

func parseAnthropicContent(raw json.RawMessage, fieldName string) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("%s is required", fieldName)
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}

	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("%s must be a string or content block array", fieldName)
	}
	if len(blocks) == 0 {
		return "", fmt.Errorf("%s must not be empty", fieldName)
	}
	if len(blocks) > maxAnthropicContentBlocks {
		return "", fmt.Errorf("%s exceeds maximum of %d content blocks", fieldName, maxAnthropicContentBlocks)
	}

	parts := make([]string, 0, len(blocks))
	for i, block := range blocks {
		part, err := blockToText(block)
		if err != nil {
			return "", fmt.Errorf("%s[%d]: %w", fieldName, i, err)
		}
		parts = append(parts, part)
	}

	return strings.Join(parts, "\n"), nil
}

func blockToText(block AnthropicContentBlock) (string, error) {
	if block.Type == "" {
		return "", fmt.Errorf("type is required")
	}

	switch block.Type {
	case "text":
		return block.Text, nil
	case "image":
		return "", fmt.Errorf("image content blocks are not supported")
	case "document":
		return "", fmt.Errorf("document content blocks are not supported")
	case "tool_use":
		if block.ID == "" {
			return "", fmt.Errorf("tool_use.id is required")
		}
		if block.Name == "" {
			return "", fmt.Errorf("tool_use.name is required")
		}
		input := compactJSON(block.Input)
		if input == "" {
			return fmt.Sprintf("[tool_use id=%s name=%s]", block.ID, block.Name), nil
		}
		return fmt.Sprintf("[tool_use id=%s name=%s input=%s]", block.ID, block.Name, input), nil
	case "tool_result":
		if block.ToolUseID == "" {
			return "", fmt.Errorf("tool_result.tool_use_id is required")
		}
		result := toolResultText(block.Content)
		if result == "" {
			return fmt.Sprintf("[tool_result id=%s]", block.ToolUseID), nil
		}
		return fmt.Sprintf("[tool_result id=%s content=%s]", block.ToolUseID, result), nil
	default:
		return "", fmt.Errorf("type %q is not supported", block.Type)
	}
}

func toolResultText(raw json.RawMessage) string {
	if isRawNull(raw) {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}

	var asBlocks []AnthropicContentBlock
	if err := json.Unmarshal(raw, &asBlocks); err == nil {
		parts := make([]string, 0, len(asBlocks))
		for _, block := range asBlocks {
			part, err := blockToText(block)
			if err != nil {
				continue
			}
			if part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	return compactJSON(raw)
}

func compactJSON(raw json.RawMessage) string {
	if isRawNull(raw) {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return ""
	}
	return buf.String()
}

func validateAnthropicOptionalFields(req AnthropicMessagesRequest) error {
	if req.TopK != nil && *req.TopK <= 0 {
		return fmt.Errorf("top_k must be > 0")
	}
	if req.TopP != nil && (*req.TopP <= 0 || *req.TopP > 1) {
		return fmt.Errorf("top_p must be > 0 and <= 1")
	}
	if req.Temperature != nil && *req.Temperature < 0 {
		return fmt.Errorf("temperature must be >= 0")
	}
	if !isRawNull(req.Thinking) {
		var thinking map[string]any
		if err := json.Unmarshal(req.Thinking, &thinking); err != nil {
			return fmt.Errorf("thinking must be an object")
		}
	}
	return nil
}

func normalizeAnthropicMetadata(metadata map[string]any) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]string, min(len(keys), maxAnthropicMetadataPairs))
	for _, k := range keys {
		if len(out) >= maxAnthropicMetadataPairs {
			break
		}
		trimmedKey := truncateRunes(k, maxAnthropicMetadataKeyLen)
		if trimmedKey == "" {
			continue
		}
		v := metadata[k]

		switch tv := v.(type) {
		case string:
			out[trimmedKey] = truncateRunes(tv, maxAnthropicMetadataValLen)
		default:
			b, err := json.Marshal(tv)
			if err != nil {
				continue
			}
			out[trimmedKey] = truncateRunes(string(b), maxAnthropicMetadataValLen)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func anthropicJSONOverrides(req AnthropicMessagesRequest) map[string]any {
	out := map[string]any{}
	if req.TopK != nil {
		out["top_k"] = *req.TopK
	}
	if !isRawNull(req.Thinking) {
		var thinking any
		if err := json.Unmarshal(req.Thinking, &thinking); err == nil {
			out["thinking"] = thinking
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildAnthropicPrompts(msgs []Message) (string, string) {
	return extractMessagesCore(msgs, true, true)
}
