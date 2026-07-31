package gateway

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dotcommander/llambo/providers"
)

func buildAnthropicResponse(result providers.ChatResult, stopSequences []string) AnthropicMessagesResponse {
	content := result.Content
	matched, matchIdx := matchStopSequence(content, stopSequences)
	if matched != nil && matchIdx >= 0 {
		content = content[:matchIdx]
	}
	content = truncateRunes(content, maxAnthropicResponseRunes)

	blocks := make([]AnthropicContentBlock, 0, 1+len(result.ToolCalls))
	if content != "" {
		blocks = append(blocks, AnthropicContentBlock{Type: "text", Text: content})
	}
	for i, tc := range result.ToolCalls {
		if i >= maxAnthropicToolUseBlocks {
			break
		}
		blocks = append(blocks, AnthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: toolInputRaw(tc.Arguments),
		})
	}
	if len(blocks) == 0 {
		blocks = append(blocks, AnthropicContentBlock{Type: "text", Text: ""})
	}

	usage := AnthropicUsage{}
	if result.Usage != nil {
		usage.InputTokens = result.Usage.PromptTokens
		usage.OutputTokens = result.Usage.CompletionTokens
	}

	return AnthropicMessagesResponse{
		ID:           anthropicID("msg"),
		Type:         "message",
		Role:         "assistant",
		Content:      blocks,
		Model:        result.Model,
		StopReason:   mapOpenAIFinishReasonToAnthropic(result.FinishReason, matched),
		StopSequence: matched,
		Usage:        usage,
	}
}

func normalizeOpenAIFinishReason(reason string) string {
	switch strings.TrimSpace(strings.ToLower(reason)) {
	case "stop", "length", "content_filter", "tool_calls":
		return strings.TrimSpace(strings.ToLower(reason))
	case "max_tokens":
		return "length"
	default:
		return "stop"
	}
}

func mapOpenAIFinishReasonToAnthropic(reason string, matchedStop *string) string {
	if matchedStop != nil {
		return "stop_sequence"
	}

	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "stop", "content_filter", "":
		return "end_turn"
	default:
		return "end_turn"
	}
}

func matchStopSequence(content string, stopSequences []string) (*string, int) {
	if content == "" || len(stopSequences) == 0 {
		return nil, -1
	}

	type stopMatch struct {
		seq string
		idx int
	}
	matches := make([]stopMatch, 0, len(stopSequences))
	for _, seq := range stopSequences {
		if seq == "" {
			continue
		}
		idx := strings.Index(content, seq)
		if idx >= 0 {
			matches = append(matches, stopMatch{seq: seq, idx: idx})
		}
	}
	if len(matches) == 0 {
		return nil, -1
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].idx == matches[j].idx {
			return len(matches[i].seq) < len(matches[j].seq)
		}
		return matches[i].idx < matches[j].idx
	})

	match := matches[0].seq
	return &match, matches[0].idx
}

func normalizeStopSequences(sequences []string) []string {
	if len(sequences) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(sequences))
	out := make([]string, 0, len(sequences))
	for _, s := range sequences {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func anthropicID(prefix string) string {
	id := generateID(prefix)
	return strings.Replace(id, prefix+"-", prefix+"_", 1)
}

func anthropicErrorTypeFromStatus(status int) string {
	switch status {
	case 400:
		return "invalid_request_error"
	case 401:
		return "authentication_error"
	case 403:
		return "permission_error"
	case 404:
		return "not_found_error"
	case 413:
		return "request_too_large"
	case 429:
		return "rate_limit_error"
	case 503, 529:
		return "overloaded_error"
	default:
		return "api_error"
	}
}

func toolInputRaw(arguments string) json.RawMessage {
	if arguments == "" {
		return json.RawMessage("{}")
	}
	if len(arguments) > maxAnthropicToolInputBytes {
		b, _ := json.Marshal(map[string]any{
			"truncated":             true,
			"raw_arguments_preview": truncateRunes(arguments, 200),
		})
		return json.RawMessage(b)
	}
	if json.Valid([]byte(arguments)) {
		return json.RawMessage(arguments)
	}
	b, _ := json.Marshal(map[string]string{"raw_arguments": arguments})
	return json.RawMessage(b)
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxRunes])
}
