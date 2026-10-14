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
	usedNativeStops := nativeStopsEnabled(request, ctx)
	start := time.Now()
	chatLog().DebugContext(ctx, "provider chat request: entry", "provider", cfg.GetProviderType(), "model", cfg.Model)

	var lastErr error
	for attempt := 0; attempt <= reqConfig.MaxRetries; attempt++ {
		attemptRequest := buildTextRequest(ctx, cfg, systemPrompt, userContent)
		attemptRequest.Stop = attemptStops(request)
		resp, err := client.Text(ctx, attemptRequest)
		if err == nil {
			reply, identity, extractErr := extractContentFromTextResponseWithIdentity(resp, cfg.Model)
			reply.usage = usageOrEstimate(reply.usage, systemPrompt, userContent, reply.content)
			logChatExit(ctx, cfg, chatExitEvent, start, chatExitResult{attempt: attempt, outcome: outcomeLabel(extractErr)})
			return reply, identity, extractErr
		}

		follow, retryErr := advanceChatRetry(ctx, &request, &usedNativeStops, reqConfig, chatRetryFailure{attempt: attempt, err: err, emitted: false})
		if follow == retryWithoutNativeStops {
			attempt--
			continue
		}
		if follow == retryStop {
			// Terminal provider error: the exhausted schedule is the retry count.
			logChatExit(ctx, cfg, chatExitEvent, start, chatExitResult{attempt: reqConfig.MaxRetries, outcome: chatOutcomeError})
			return chatReply{}, chatResponseIdentity{}, retryErr
		}
		if follow == retryBackoffInterrupted {
			logChatExit(ctx, cfg, chatExitEvent, start, chatExitResult{attempt: attempt, outcome: chatOutcomeError})
			return chatReply{}, chatResponseIdentity{}, retryErr
		}
		lastErr = retryErr
	}

	logChatExit(ctx, cfg, chatExitEvent, start, chatExitResult{attempt: reqConfig.MaxRetries, outcome: chatOutcomeError})
	return chatReply{}, chatResponseIdentity{}, lastErr
}

// executeChatStreamRequestWithIdentity runs one streaming chat attempt with
// the same retry policy as the non-streaming path. Each attempt consumes the
// stream under its own cancellable context so an in-flight stream is torn
// down before a retry or failover begins.
func executeChatStreamRequestWithIdentity(
	ctx context.Context,
	client whtypes.Provider,
	call chatAttempt,
	onChunk ChatStreamHandler,
	reqConfig ChatRequestConfig,
) chatStreamOutcome {
	cfg, systemPrompt, userContent := call.cfg, call.systemPrompt, call.userContent
	request := buildTextRequest(ctx, cfg, systemPrompt, userContent)
	usedNativeStops := nativeStopsEnabled(request, ctx)
	start := time.Now()
	chatLog().DebugContext(ctx, "provider chat stream request: entry", "provider", cfg.GetProviderType(), "model", cfg.Model)

	var lastErr error
	for attempt := 0; attempt <= reqConfig.MaxRetries; attempt++ {
		attemptCtx, cancel := context.WithCancel(ctx)
		attemptRequest := buildTextRequest(ctx, cfg, systemPrompt, userContent)
		attemptRequest.Stop = attemptStops(request)
		stream, err := client.Stream(attemptCtx, attemptRequest)
		if err != nil {
			cancel()
			follow, retryErr := advanceChatRetry(ctx, &request, &usedNativeStops, reqConfig, chatRetryFailure{attempt: attempt, err: err, emitted: false})
			if follow == retryWithoutNativeStops {
				attempt--
				continue
			}
			if follow == retryStop || follow == retryBackoffInterrupted {
				logChatExit(ctx, cfg, chatStreamExitEvent, start, chatExitResult{attempt: attempt, outcome: chatOutcomeError})
				return chatStreamOutcome{err: retryErr}
			}
			lastErr = retryErr
			continue
		}

		outcome := consumeTextStream(stream, cfg.Model, onChunk)
		cancel()
		if outcome.err == nil {
			logChatExit(ctx, cfg, chatStreamExitEvent, start, chatExitResult{attempt: attempt, outcome: chatOutcomeSuccess})
			outcome.reply.usage = usageOrEstimate(outcome.reply.usage, systemPrompt, userContent, outcome.reply.content)
			return outcome
		}

		follow, retryErr := advanceChatRetry(ctx, &request, &usedNativeStops, reqConfig, chatRetryFailure{attempt: attempt, err: outcome.err, emitted: outcome.emitted})
		if follow == retryWithoutNativeStops {
			attempt--
			continue
		}
		if follow == retryStop || follow == retryBackoffInterrupted {
			logChatExit(ctx, cfg, chatStreamExitEvent, start, chatExitResult{attempt: attempt, outcome: chatOutcomeError})
			return chatStreamOutcome{emitted: outcome.emitted, err: retryErr}
		}
		lastErr = retryErr
	}

	logChatExit(ctx, cfg, chatStreamExitEvent, start, chatExitResult{attempt: reqConfig.MaxRetries, outcome: chatOutcomeError})
	return chatStreamOutcome{err: lastErr}
}
