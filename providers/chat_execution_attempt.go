package providers

import (
	"context"
	"fmt"
	"time"
)

// chatAttempt bundles the per-attempt routing inputs threaded through the
// chat execution helpers.
type chatAttempt struct {
	backendName  string
	cfg          Config
	systemPrompt string
	userContent  string
}

// chatReply carries the provider-independent reply components of one chat
// execution before identity enrichment.
type chatReply struct {
	content      string
	usage        *LLMUsage
	finishReason string
	toolCalls    []ToolCall
}

func executeChatAttemptWithKeyRotation(ctx context.Context, call chatAttempt, rotateKey func(string, chatRequestResult) (bool, error), request func(context.Context, chatAttempt) (chatRequestResult, error)) (chatRequestResult, error) {
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return chatRequestResult{}, err
		}
		result, err := request(ctx, call)
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
		rotated, rotationErr := rotateKey(call.backendName, result)
		if rotationErr != nil {
			return result, rotationErr
		}
		if !rotated {
			return result, err
		}
	}

}

func finalizeChatRequest(call chatAttempt, start time.Time, reply chatReply, err error) (chatRequestResult, error) {
	backendName, cfg, usage := call.backendName, call.cfg, reply.usage
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
	result.content = reply.content
	result.usage = usage
	result.finishReason = reply.finishReason
	result.toolCalls = reply.toolCalls
	return result, nil
}
