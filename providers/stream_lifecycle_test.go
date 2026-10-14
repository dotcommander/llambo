package providers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func TestConsumeTextStream_AbortsUntrustworthyToolIndex(t *testing.T) {
	t.Parallel()
	stream := make(chan whtypes.TextChunk, 2)
	stream <- whtypes.TextChunk{Text: "hello"}
	stream <- whtypes.TextChunk{ToolCalls: []whtypes.ToolCall{{Index: maxStreamToolIndex + 1, ID: "call", Function: &whtypes.ToolCallFunction{Name: "fn"}}}}
	close(stream)

	result := consumeTextStream(stream, "model", nil)
	if result.err == nil || !strings.Contains(result.err.Error(), "invalid tool index") {
		t.Fatalf("expected invalid tool index abort, got %v", result.err)
	}
	if result.reply.toolCalls != nil {
		t.Fatalf("untrustworthy index must suppress aggregated calls, got %+v", result.reply.toolCalls)
	}
	if result.reply.content != "" || !result.emitted {
		t.Fatalf("content=%q emitted=%v, want empty content with earlier emission retained", result.reply.content, result.emitted)
	}
}

func TestConsumeTextStream_WireErrorKeepsPartialToolCalls(t *testing.T) {
	t.Parallel()
	cause := assertErr("upstream socket closed")
	stream := make(chan whtypes.TextChunk, 2)
	stream <- whtypes.TextChunk{ToolCalls: []whtypes.ToolCall{{Index: 1, ID: "call", Function: &whtypes.ToolCallFunction{Arguments: "{"}}}}
	stream <- whtypes.TextChunk{Error: cause}
	close(stream)

	result := consumeTextStream(stream, "model", nil)
	if !errors.Is(result.err, cause) {
		t.Fatalf("expected wire error propagated, got %v", result.err)
	}
	if len(result.reply.toolCalls) != 1 || result.reply.toolCalls[0].Arguments != "{" {
		t.Fatalf("partial indexed calls must survive a wire abort, got %+v", result.reply.toolCalls)
	}
}

func TestConsumeTextStream_EmptyStreamIsFailedGeneration(t *testing.T) {
	t.Parallel()
	stream := make(chan whtypes.TextChunk, 1)
	stream <- whtypes.TextChunk{}
	close(stream)

	result := consumeTextStream(stream, "model", nil)
	if result.err == nil || !strings.Contains(result.err.Error(), "no content in streaming response") {
		t.Fatalf("expected no-content failure, got %v", result.err)
	}
	if result.reply.content != "" || result.reply.toolCalls != nil || result.emitted {
		t.Fatalf("empty generation must report nothing: content=%q calls=%+v emitted=%v", result.reply.content, result.reply.toolCalls, result.emitted)
	}
}

func TestWireToolCalls_ShapePrecedence(t *testing.T) {
	t.Parallel()
	batched := []whtypes.ToolCall{{Index: 0, ID: "a"}}
	if got := wireToolCalls(whtypes.TextChunk{ToolCalls: batched, ToolCall: &whtypes.ToolCall{ID: "ignored"}}); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("batched calls must win: %+v", got)
	}
	if got := wireToolCalls(whtypes.TextChunk{Delta: &whtypes.ChunkDelta{ToolCalls: batched}}); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("delta-attached calls must be used: %+v", got)
	}
	single := wireToolCalls(whtypes.TextChunk{ToolCall: &whtypes.ToolCall{ID: "solo"}})
	if len(single) != 1 || single[0].ID != "solo" {
		t.Fatalf("single call must be wrapped: %+v", single)
	}
	if got := wireToolCalls(whtypes.TextChunk{}); got != nil {
		t.Fatalf("shapeless chunk must yield no calls: %+v", got)
	}
}

func TestStreamWorkerCount_ClampsOnlyDownward(t *testing.T) {
	t.Parallel()
	if got := streamWorkerCount(10, 2); got != 2 {
		t.Fatalf("maxWorkers must tighten the pool: got %d", got)
	}
	if got := streamWorkerCount(10, 0); got != 10 {
		t.Fatalf("zero maxWorkers must use the configured pool: got %d", got)
	}
	if got := streamWorkerCount(10, 20); got != 10 {
		t.Fatalf("maxWorkers must never exceed the configured pool: got %d", got)
	}
}

func TestAdvanceChatRetry_NativeStopFallbackClearsStops(t *testing.T) {
	t.Parallel()
	request := whtypes.TextRequest{BaseRequest: whtypes.BaseRequest{Stop: []string{"END"}}}
	usedNativeStops := true

	follow, retryErr := advanceChatRetry(context.Background(), &request, &usedNativeStops, DefaultChatConfig, chatRetryFailure{err: assertErr("stop is not supported")})
	if follow != retryWithoutNativeStops || retryErr != nil {
		t.Fatalf("fallback = (%v, %v), want retry without native stops", follow, retryErr)
	}
	if len(request.Stop) != 0 || usedNativeStops {
		t.Fatalf("stops must be cleared in place: stop=%+v used=%v", request.Stop, usedNativeStops)
	}
}

func TestAdvanceChatRetry_BackoffAndTerminalCancellation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		config := ChatRequestConfig{MaxRetries: 3, Backoffs: []time.Duration{time.Hour}}
		request := whtypes.TextRequest{BaseRequest: whtypes.BaseRequest{Stop: []string{"END"}}}
		usedNativeStops := true
		cause := assertErr("connection reset by peer")

		follow, wrapped := advanceChatRetry(context.Background(), &request, &usedNativeStops, config, chatRetryFailure{err: cause})
		if follow != retryAfterBackoff || wrapped == nil {
			t.Fatalf("live context must back off and carry the wrapped error: follow=%v wrapped=%v", follow, wrapped)
		}
		if len(request.Stop) == 0 || !usedNativeStops {
			t.Fatal("backoff retry must retain native stops")
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		follow, retryErr := advanceChatRetry(ctx, &request, &usedNativeStops, config, chatRetryFailure{err: ctx.Err()})
		if follow != retryStop || !errors.Is(retryErr, context.Canceled) {
			t.Fatalf("cancellation must terminate retries: follow=%v err=%v", follow, retryErr)
		}
	})
}
