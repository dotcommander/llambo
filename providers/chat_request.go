package providers

import (
	"context"
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

func decideChatRetry(err error, attempt, maxRetries int, usedNativeStops, emitted bool) (chatRetryAction, error) {
	if usedNativeStops && !emitted && isUnsupportedStopError(err) {
		return chatRetryWithoutNativeStops, nil
	}
	wrapped := wrapProviderError(err)
	if emitted || !IsRetryable(wrapped) || attempt >= maxRetries {
		return chatRetryStop, wrapped
	}
	return chatRetryAfterBackoff, wrapped
}

// executeChatRequestWithIdentity preserves the provider/model declared by
// wormhole's response. The public compatibility wrapper intentionally
// retains the pre-existing return shape for non-evaluation callers.
func executeChatRequestWithIdentity(
	ctx context.Context,
	client whtypes.Provider,
	call chatAttempt,
	reqConfig ChatRequestConfig,
) (chatReply, chatResponseIdentity, error) {
	cfg, systemPrompt, userContent := call.cfg, call.systemPrompt, call.userContent
	request := buildTextRequest(ctx, cfg, systemPrompt, userContent)
	usedNativeStops := len(request.Stop) > 0
	if _, structured := ctx.Value(structuredRequestKey{}).(StructuredChatRequest); structured {
		usedNativeStops = false
	}
	start := time.Now()
	chatLog().DebugContext(ctx, "provider chat request: entry", "provider", cfg.GetProviderType(), "model", cfg.Model)

	var lastErr error
	for attempt := 0; attempt <= reqConfig.MaxRetries; attempt++ {
		attemptRequest := buildTextRequest(ctx, cfg, systemPrompt, userContent)
		if len(request.Stop) == 0 {
			attemptRequest.Stop = nil
		}
		resp, err := client.Text(ctx, attemptRequest)
		if err == nil {
			reply, identity, extractErr := extractContentFromTextResponseWithIdentity(resp, cfg.Model)
			reply.usage = usageOrEstimate(reply.usage, systemPrompt, userContent, reply.content)
			err = extractErr
			outcome := "success"
			if err != nil {
				outcome = "error"
			}
			chatLog().DebugContext(ctx, "provider chat request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", outcome)
			return reply, identity, err
		}

		action, wrapped := decideChatRetry(err, attempt, reqConfig.MaxRetries, usedNativeStops, false)
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
		if err = waitChatBackoff(ctx, reqConfig, attempt); err != nil {
			chatLog().DebugContext(ctx, "provider chat request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
			return chatReply{}, chatResponseIdentity{}, err
		}
	}

	chatLog().DebugContext(ctx, "provider chat request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", reqConfig.MaxRetries, "outcome", "error")
	return chatReply{}, chatResponseIdentity{}, lastErr
}

func executeChatStreamRequestWithIdentity(
	ctx context.Context,
	client whtypes.Provider,
	call chatAttempt,
	onChunk ChatStreamHandler,
	reqConfig ChatRequestConfig,
) (chatReply, bool, chatResponseIdentity, error) {
	cfg, systemPrompt, userContent := call.cfg, call.systemPrompt, call.userContent
	request := buildTextRequest(ctx, cfg, systemPrompt, userContent)
	usedNativeStops := len(request.Stop) > 0
	if _, structured := ctx.Value(structuredRequestKey{}).(StructuredChatRequest); structured {
		usedNativeStops = false
	}
	start := time.Now()
	chatLog().DebugContext(ctx, "provider chat stream request: entry", "provider", cfg.GetProviderType(), "model", cfg.Model)

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
			action, wrapped := decideChatRetry(err, attempt, reqConfig.MaxRetries, usedNativeStops, false)
			if action == chatRetryWithoutNativeStops {
				request.Stop = nil
				usedNativeStops = false
				attempt--
				continue
			}
			lastErr = wrapped
			if action == chatRetryStop {
				chatLog().DebugContext(ctx, "provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
				return chatReply{}, false, chatResponseIdentity{}, lastErr
			}
			if err = waitChatBackoff(ctx, reqConfig, attempt); err != nil {
				chatLog().DebugContext(ctx, "provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
				return chatReply{}, false, chatResponseIdentity{}, err
			}
			continue
		}

		content, usage, finishReason, toolCalls, emitted, identity, err := consumeTextStream(stream, cfg.Model, onChunk)
		cancel()
		if err == nil {
			chatLog().DebugContext(ctx, "provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "success")
			return chatReply{content: content, usage: usageOrEstimate(usage, systemPrompt, userContent, content), finishReason: finishReason, toolCalls: toolCalls}, emitted, identity, nil
		}

		action, wrapped := decideChatRetry(err, attempt, reqConfig.MaxRetries, usedNativeStops, emitted)
		if action == chatRetryWithoutNativeStops {
			request.Stop = nil
			usedNativeStops = false
			attempt--
			continue
		}

		lastErr = wrapped
		if action == chatRetryStop {
			chatLog().DebugContext(ctx, "provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
			return chatReply{}, emitted, chatResponseIdentity{}, lastErr
		}
		if err := waitChatBackoff(ctx, reqConfig, attempt); err != nil {
			chatLog().DebugContext(ctx, "provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", attempt, "outcome", "error")
			return chatReply{}, emitted, chatResponseIdentity{}, err
		}
	}

	chatLog().DebugContext(ctx, "provider chat stream request: exit", "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", reqConfig.MaxRetries, "outcome", "error")
	return chatReply{}, false, chatResponseIdentity{}, lastErr
}
