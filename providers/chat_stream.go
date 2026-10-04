package providers

import (
	"fmt"
	"strings"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func consumeTextStream(stream <-chan whtypes.TextChunk, model string, onChunk ChatStreamHandler) (string, *LLMUsage, string, []ToolCall, bool, chatResponseIdentity, error) {
	var contentBuilder strings.Builder
	toolCalls := make([]ToolCall, 0)
	finishReason := ""
	var usage *LLMUsage
	emitted := false
	identity := chatResponseIdentity{}

	for chunk := range stream {
		if chunk.Error != nil {
			return "", usage, finishReason, compactToolCalls(toolCalls), emitted, identity, chunk.Error
		}
		if chunk.Provider != "" {
			identity.provider = chunk.Provider
		}
		if chunk.Model != "" {
			identity.model = chunk.Model
		}
		if chunk.Usage != nil {
			usage = usageFromWormhole(chunk.Usage)
		}

		out := ChatStreamChunk{}
		out.Provider = chunk.Provider
		out.Model = chunk.Model
		if delta := chunk.Content(); delta != "" {
			contentBuilder.WriteString(delta)
			out.ContentDelta = delta
		}

		rawCalls := chunk.ToolCalls
		if len(rawCalls) == 0 && chunk.Delta != nil {
			rawCalls = chunk.Delta.ToolCalls
		}
		if len(rawCalls) == 0 && chunk.ToolCall != nil {
			rawCalls = []whtypes.ToolCall{*chunk.ToolCall}
		}
		for _, raw := range rawCalls {
			tc := convertWormholeToolCalls([]whtypes.ToolCall{raw})[0]
			index := raw.Index
			if index < 0 || index > 4096 {
				return "", usage, finishReason, nil, emitted, identity, fmt.Errorf("invalid tool index %d", index)
			}
			for len(toolCalls) <= index {
				toolCalls = append(toolCalls, ToolCall{})
			}
			current := &toolCalls[index]
			if tc.ID != "" {
				current.ID = tc.ID
			}
			if tc.Name != "" {
				current.Name = tc.Name
			}
			current.Arguments += tc.Arguments
			out.ToolDeltas = append(out.ToolDeltas, ToolCallDelta{Index: index, ID: tc.ID, Name: tc.Name, ArgumentsDelta: tc.Arguments})
		}

		if chunk.FinishReason != nil {
			finishReason = string(*chunk.FinishReason)
			out.FinishReason = finishReason
		}

		if out.ContentDelta != "" || len(out.ToolDeltas) > 0 || out.FinishReason != "" {
			emitted = true
			if onChunk != nil {
				if err := onChunk(out); err != nil {
					return "", usage, finishReason, compactToolCalls(toolCalls), emitted, identity, &ConsumerError{Err: err}
				}
			}
		}
	}

	content := strings.TrimSpace(contentBuilder.String())
	if content == "" && len(toolCalls) == 0 {
		return "", usage, finishReason, nil, emitted, identity, fmt.Errorf("%s: no content in streaming response", model)
	}

	return content, usage, finishReason, compactToolCalls(toolCalls), emitted, identity, nil
}

func compactToolCalls(in []ToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(in))
	for _, tc := range in {
		if tc.ID == "" && tc.Name == "" && tc.Arguments == "" {
			continue
		}
		out = append(out, tc)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
