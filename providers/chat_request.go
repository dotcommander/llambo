package providers

import (
	"context"
	"log/slog"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

const maxNativeStopSequences = 4

// ChatRequestConfig holds configuration for chat requests
type ChatRequestConfig struct {
	Timeout    time.Duration
	MaxRetries int
	Backoffs   []time.Duration
}

// DefaultChatConfig provides sensible defaults for chat requests
var DefaultChatConfig = ChatRequestConfig{
	Timeout:    DefaultRequestTimeout,
	MaxRetries: DefaultRetryCount,
	Backoffs:   DefaultRetryBackoffs,
}

type chatRetryAction uint8

const (
	chatRetryStop chatRetryAction = iota
	chatRetryAfterBackoff
	chatRetryWithoutNativeStops
)

func decideChatRetry(err error, cfg Config, attempt, maxRetries int, usedNativeStops, emitted bool) (chatRetryAction, error) {
	if usedNativeStops && !emitted && isUnsupportedStopError(err) {
		return chatRetryWithoutNativeStops, nil
	}
	wrapped := wrapProviderError(err, cfg)
	if emitted || !IsRetryable(wrapped) || attempt >= maxRetries {
		return chatRetryStop, wrapped
	}
	return chatRetryAfterBackoff, wrapped
}

// ExecuteChatRequest sends a chat request through a wormhole provider with llambo-owned retry logic.
// It handles parameter building, transient error retries, and usage/tool-call mapping.
// Returns content, usage (may be nil), finish reason, tool calls (if any), and error.
func ExecuteChatRequest(
	ctx context.Context,
	client whtypes.Provider,
	cfg Config,
	systemPrompt, userContent string,
	reqConfig ChatRequestConfig,
) (string, *LLMUsage, string, []ToolCall, error) {
	content, usage, finishReason, toolCalls, _, err := executeChatRequestWithIdentity(ctx, client, cfg, systemPrompt, userContent, reqConfig)
	return content, usage, finishReason, toolCalls, err
}

// executeChatRequestWithIdentity preserves the provider/model declared by
// wormhole's response. The public compatibility wrapper above intentionally
// retains the pre-existing return shape for non-evaluation callers.
func executeChatRequestWithIdentity(
	ctx context.Context,
	client whtypes.Provider,
	cfg Config,
	systemPrompt, userContent string,
	reqConfig ChatRequestConfig,
) (string, *LLMUsage, string, []ToolCall, chatResponseIdentity, error) {
	request := buildTextRequest(ctx, cfg, systemPrompt, userContent)
	usedNativeStops := len(request.Stop) > 0
	if _, structured := ctx.Value(structuredRequestKey{}).(StructuredChatRequest); structured {
		usedNativeStops = false
	}
	start := time.Now()
	slog.Debug("provider chat request: entry", "provider", cfg.GetProviderType(), "model", cfg.Model)

	var lastErr error
	for attempt := 0; attempt <= reqConfig.MaxRetries; attempt++ {
		attemptRequest := buildTextRequest(ctx, cfg, systemPrompt, userContent)
		if len(request.Stop) == 0 {
			attemptRequest.Stop = nil
		}
		resp, err := client.Text(ctx, attemptRequest)
		if err == nil {
			content, usage, finishReason, toolCalls, identity, err := extractContentFromTextResponseWithIdentity(resp, cfg.Model)
			outcome := "success"
			if err != nil {
				outcome = "error"
			}
			slog.Debug("provider chat request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", outcome)
			return content, usageOrEstimate(usage, systemPrompt, userContent, content), finishReason, toolCalls, identity, err
		}

		action, wrapped := decideChatRetry(err, cfg, attempt, reqConfig.MaxRetries, usedNativeStops, false)
		if action == chatRetryWithoutNativeStops {
			request.Stop = nil
			usedNativeStops = false
			attempt--
			continue
		}

		lastErr = wrapped
		if action == chatRetryStop {
			break
		}
		if err := waitChatBackoff(ctx, reqConfig, attempt); err != nil {
			slog.Debug("provider chat request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
			return "", nil, "", nil, chatResponseIdentity{}, err
		}
	}

	slog.Debug("provider chat request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", reqConfig.MaxRetries, "outcome", "error")
	return "", nil, "", nil, chatResponseIdentity{}, lastErr
}

// ExecuteChatStreamRequest sends a streaming chat request and forwards deltas.
// Returns full content, usage, finish reason, tool calls, whether any chunk was emitted, and error.
func ExecuteChatStreamRequest(
	ctx context.Context,
	client whtypes.Provider,
	cfg Config,
	systemPrompt, userContent string,
	onChunk ChatStreamHandler,
	reqConfig ChatRequestConfig,
) (string, *LLMUsage, string, []ToolCall, bool, error) {
	content, usage, finishReason, toolCalls, emitted, _, err := executeChatStreamRequestWithIdentity(ctx, client, cfg, systemPrompt, userContent, onChunk, reqConfig)
	return content, usage, finishReason, toolCalls, emitted, err
}

func executeChatStreamRequestWithIdentity(
	ctx context.Context,
	client whtypes.Provider,
	cfg Config,
	systemPrompt, userContent string,
	onChunk ChatStreamHandler,
	reqConfig ChatRequestConfig,
) (string, *LLMUsage, string, []ToolCall, bool, chatResponseIdentity, error) {
	request := buildTextRequest(ctx, cfg, systemPrompt, userContent)
	usedNativeStops := len(request.Stop) > 0
	if _, structured := ctx.Value(structuredRequestKey{}).(StructuredChatRequest); structured {
		usedNativeStops = false
	}
	start := time.Now()
	slog.Debug("provider chat stream request: entry", "provider", cfg.GetProviderType(), "model", cfg.Model)

	var lastErr error
	for attempt := 0; attempt <= reqConfig.MaxRetries; attempt++ {
		attemptCtx, cancel := context.WithCancel(ctx)
		attemptRequest := buildTextRequest(ctx, cfg, systemPrompt, userContent)
		if len(request.Stop) == 0 {
			attemptRequest.Stop = nil
		}
		stream, err := client.Stream(attemptCtx, attemptRequest)
		if err != nil {
			cancel()
			action, wrapped := decideChatRetry(err, cfg, attempt, reqConfig.MaxRetries, usedNativeStops, false)
			if action == chatRetryWithoutNativeStops {
				request.Stop = nil
				usedNativeStops = false
				attempt--
				continue
			}
			lastErr = wrapped
			if action == chatRetryStop {
				slog.Debug("provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
				return "", nil, "", nil, false, chatResponseIdentity{}, lastErr
			}
			if err := waitChatBackoff(ctx, reqConfig, attempt); err != nil {
				slog.Debug("provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
				return "", nil, "", nil, false, chatResponseIdentity{}, err
			}
			continue
		}

		content, usage, finishReason, toolCalls, emitted, identity, err := consumeTextStream(stream, cfg.Model, onChunk)
		cancel()
		if err == nil {
			slog.Debug("provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "success")
			return content, usageOrEstimate(usage, systemPrompt, userContent, content), finishReason, toolCalls, emitted, identity, nil
		}

		action, wrapped := decideChatRetry(err, cfg, attempt, reqConfig.MaxRetries, usedNativeStops, emitted)
		if action == chatRetryWithoutNativeStops {
			request.Stop = nil
			usedNativeStops = false
			attempt--
			continue
		}

		lastErr = wrapped
		if action == chatRetryStop {
			slog.Debug("provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
			return "", nil, "", nil, emitted, chatResponseIdentity{}, lastErr
		}
		if err := waitChatBackoff(ctx, reqConfig, attempt); err != nil {
			slog.Debug("provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
			return "", nil, "", nil, emitted, chatResponseIdentity{}, err
		}
	}

	slog.Debug("provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", reqConfig.MaxRetries, "outcome", "error")
	return "", nil, "", nil, false, chatResponseIdentity{}, lastErr
}
