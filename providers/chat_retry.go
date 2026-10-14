package providers

import (
	"context"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

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

// chatRetryFollowUp directs the retry loop after a failed attempt.
type chatRetryFollowUp uint8

const (
	// retryWithoutNativeStops redoes the same attempt with native stop
	// sequences dropped after the provider rejected them.
	retryWithoutNativeStops chatRetryFollowUp = iota
	// retryAfterBackoff waits out the backoff schedule, then advances to the
	// next attempt; the second return value is the wrapped provider error.
	retryAfterBackoff
	// retryStop ends the loop: the wrapped provider error is final.
	retryStop
	// retryBackoffInterrupted ends the loop because the backoff wait hit a
	// cancelled context; the second return value is that wait error.
	retryBackoffInterrupted
)

type chatRetryFailure struct {
	attempt int
	err     error
	emitted bool
}

// advanceChatRetry applies the retry policy to one failed attempt. It clears
// native stops in place when the provider rejects them and waits out the
// backoff schedule before another attempt, so callers only branch on the
// follow-up. Retryable classification, the native-stop fallback, and backoff
// ordering are owned here and stay identical across both chat paths.
func advanceChatRetry(ctx context.Context, request *whtypes.TextRequest, usedNativeStops *bool, reqConfig ChatRequestConfig, failure chatRetryFailure) (chatRetryFollowUp, error) {
	attempt := failure.attempt
	action, wrapped := decideChatRetry(failure.err, attempt, reqConfig.MaxRetries, *usedNativeStops, failure.emitted)
	var follow chatRetryFollowUp
	switch action {
	case chatRetryWithoutNativeStops:
		request.Stop = nil
		*usedNativeStops = false
		follow = retryWithoutNativeStops
	case chatRetryAfterBackoff:
		if waitErr := waitChatBackoff(ctx, reqConfig, attempt); waitErr != nil {
			return retryBackoffInterrupted, waitErr
		}
		follow = retryAfterBackoff
	case chatRetryStop:
		follow = retryStop
	}
	return follow, wrapped
}

// nativeStopsEnabled reports whether the wire request carries native stop
// sequences; structured requests always filter stops client-side.
func nativeStopsEnabled(request whtypes.TextRequest, ctx context.Context) bool {
	if _, structured := ctx.Value(structuredRequestKey{}).(StructuredChatRequest); structured {
		return false
	}
	return len(request.Stop) > 0
}

// attemptStops returns the stop sequences for one attempt: nil unless the
// master request still carries native stops (they may have been dropped after
// an unsupported-stop rejection).
func attemptStops(request whtypes.TextRequest) []string {
	if len(request.Stop) == 0 {
		return nil
	}
	return request.Stop
}

// outcomeLabel names a chat attempt's telemetry outcome.
func outcomeLabel(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

type chatExitResult struct {
	attempt int
	outcome string
}

// logChatExit emits the uniform exit telemetry for a chat attempt; event
// selects the plain or streaming request log line.
func logChatExit(ctx context.Context, cfg Config, event string, start time.Time, result chatExitResult) {
	chatLog().DebugContext(ctx, event, "provider", cfg.GetProviderType(), "model", cfg.Model, "latency", time.Since(start), "retry_count", result.attempt, "outcome", result.outcome)
}

const (
	chatExitEvent       = "provider chat request: exit"
	chatStreamExitEvent = "provider chat stream request: exit"
	chatOutcomeError    = "error"
	chatOutcomeSuccess  = "success"
)
