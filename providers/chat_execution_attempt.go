package providers

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

func executeChatAttemptWithKeyRotation(ctx context.Context, backendName string, cfg Config, systemPrompt, userContent string, rotateKey func(string, chatRequestResult) (bool, error), request func(context.Context, string, Config, string, string) (chatRequestResult, error)) (chatRequestResult, error) {
	slog.Debug("provider attempt: entry", "provider", backendName, "model", cfg.Model)
	result, err := request(ctx, backendName, cfg, systemPrompt, userContent)
	if err == nil {
		slog.Debug("provider attempt: exit", "provider", backendName, "model", cfg.Model, "latency", result.duration, "retry_count", 0, "outcome", "success")
		return result, nil
	}
	if rotateKey != nil && IsRateLimitError(err) {
		rotated, rotErr := rotateKey(backendName, result)
		if rotated {
			retryResult, retryErr := request(ctx, backendName, cfg, systemPrompt, userContent)
			outcome := "success"
			if retryErr != nil {
				outcome = "error"
			}
			slog.Debug("provider attempt: exit", "provider", backendName, "model", cfg.Model, "latency", retryResult.duration, "retry_count", 1, "outcome", outcome)
			return retryResult, retryErr
		}
		// A client-construction failure during rotation is NOT a quota event;
		// propagate the distinct error so the circuit breaker stays healthy.
		if rotErr != nil {
			slog.Debug("provider attempt: exit", "provider", backendName, "model", cfg.Model, "latency", result.duration, "retry_count", 0, "outcome", "error")
			return result, rotErr
		}
	}
	slog.Debug("provider attempt: exit", "provider", backendName, "model", cfg.Model, "latency", result.duration, "retry_count", 0, "outcome", "error")
	return result, err
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
