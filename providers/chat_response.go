package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func extractContentFromTextResponse(resp *whtypes.TextResponse, model string) (string, *LLMUsage, string, []ToolCall, error) {
	if resp == nil {
		return "", nil, "", nil, fmt.Errorf("%s: empty response", model)
	}
	toolCalls := convertWormholeToolCalls(resp.ToolCalls)
	if resp.Text == "" && len(toolCalls) == 0 {
		usage := usageFromWormhole(resp.Usage)
		return "", usage, string(resp.FinishReason), nil, &NoContentResponseError{
			Model:        model,
			FinishReason: string(resp.FinishReason),
			Usage:        usage,
		}
	}
	return strings.TrimSpace(resp.Text), usageFromWormhole(resp.Usage), string(resp.FinishReason), toolCalls, nil
}

func usageFromWormhole(usage *whtypes.Usage) *LLMUsage {
	if usage == nil {
		return nil
	}
	return &LLMUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}
}

func usageOrEstimate(usage *LLMUsage, systemPrompt, userContent, content string) *LLMUsage {
	if usage != nil {
		return usage
	}
	promptTokens := max(1, EstimatePromptTokens(systemPrompt, userContent))
	completionTokens := max(1, EstimatePromptTokens("", content))
	return &LLMUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}

func wormholeToolCalls(chunk whtypes.TextChunk) []ToolCall {
	if len(chunk.ToolCalls) > 0 {
		return convertWormholeToolCalls(chunk.ToolCalls)
	}
	if chunk.ToolCall != nil {
		return convertWormholeToolCalls([]whtypes.ToolCall{*chunk.ToolCall})
	}
	if chunk.Delta != nil && len(chunk.Delta.ToolCalls) > 0 {
		return convertWormholeToolCalls(chunk.Delta.ToolCalls)
	}
	return nil
}

func convertWormholeToolCalls(in []whtypes.ToolCall) []ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(in))
	for _, tc := range in {
		args := ""
		if tc.Function != nil {
			args = tc.Function.Arguments
		}
		if args == "" && len(tc.Arguments) > 0 {
			if b, err := json.Marshal(tc.Arguments); err == nil {
				args = string(b)
			}
		}
		name := tc.Name
		if name == "" && tc.Function != nil {
			name = tc.Function.Name
		}
		out = append(out, ToolCall{
			ID:        tc.ID,
			Name:      name,
			Arguments: args,
		})
	}
	return out
}

func wrapProviderError(err error, cfg Config) *OpenAIError {
	if err == nil {
		return nil
	}
	statusCode := 0
	var wormholeErr *whtypes.WormholeError
	retryAfter := time.Duration(0)
	if errors.As(err, &wormholeErr) {
		statusCode = wormholeErr.StatusCode
		retryAfter = wormholeErr.RetryAfter
	}
	if statusCode == 0 {
		statusCode = extractHTTPStatus(err.Error())
	}
	return NewOpenAIErrorWithRetryAfter(err.Error(), statusCode, retryAfter, err)
}

func intPtr(v int) *int {
	return &v
}

func float32Ptr(v float32) *float32 {
	return &v
}

func isUnsupportedStopError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "stop") && (strings.Contains(msg, "not supported") || strings.Contains(msg, "unsupported") || strings.Contains(msg, "invalid"))
}
