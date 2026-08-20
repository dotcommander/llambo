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

		chunkToolCalls := wormholeToolCalls(chunk)
		if len(chunkToolCalls) > 0 {
			out.ToolDeltas = make([]ToolCallDelta, 0, len(chunkToolCalls))
			for _, tc := range chunkToolCalls {
				toolCalls = append(toolCalls, tc)
				out.ToolDeltas = append(out.ToolDeltas, ToolCallDelta{
					Index:          len(toolCalls) - 1,
					ID:             tc.ID,
					Name:           tc.Name,
					ArgumentsDelta: tc.Arguments,
				})
			}
		}

		if chunk.FinishReason != nil {
			finishReason = string(*chunk.FinishReason)
			out.FinishReason = finishReason
		}

		if out.ContentDelta != "" || len(out.ToolDeltas) > 0 || out.FinishReason != "" {
			emitted = true
			if onChunk != nil {
				if err := onChunk(out); err != nil {
					return "", usage, finishReason, compactToolCalls(toolCalls), emitted, identity, err
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
