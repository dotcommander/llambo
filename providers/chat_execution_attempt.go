package providers

import (
	"context"
	"fmt"
	"time"
)

func executeChatAttemptWithKeyRotation(ctx context.Context, backendName string, cfg Config, systemPrompt, userContent string, rotateKey func(string, chatRequestResult) (bool, error), request func(context.Context, string, Config, string, string) (chatRequestResult, error)) (chatRequestResult, error) {
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return chatRequestResult{}, err
		}
		result, err := request(ctx, backendName, cfg, systemPrompt, userContent)
		if err != nil && ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err == nil || rotateKey == nil || !IsRateLimitError(err) {
			return result, err
		}
		if seen[result.clientKey] {
			return result, err
		}
		seen[result.clientKey] = true
		rotated, rotationErr := rotateKey(backendName, result)
		if rotationErr != nil {
			return result, rotationErr
		}
		if !rotated {
			return result, err
		}
	}

}

func finalizeChatRequest(backendName string, cfg Config, start time.Time, content string, usage *LLMUsage, finishReason string, toolCalls []ToolCall, err error) (chatRequestResult, error) {
	result := chatRequestResult{duration: time.Since(start)}
	if err != nil {
		return result, fmt.Errorf("%s: %w", backendName, err)
	}
	if usage != nil && usage.Cost == nil {
		if usage.PromptTokens > 0 && usage.CompletionTokens > 0 {
			usage.Cost = &LLMCost{TotalCost: costFromTokenSplit(cfg, usage.PromptTokens, usage.CompletionTokens)}
		} else {
			usage.Cost = &LLMCost{TotalCost: estimateCostUSD(cfg, usage.TotalTokens)}
		}
	}
	result.content = content
	result.usage = usage
	result.finishReason = finishReason
	result.toolCalls = toolCalls
	return result, nil
}
