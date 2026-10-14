package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func TestExecuteChatRequestCompatibility(t *testing.T) {
	t.Parallel()
	client := &fakeTextProvider{resp: &whtypes.TextResponse{
		Text: " hello ", FinishReason: whtypes.FinishReasonStop,
		Usage: &whtypes.Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
	}}
	content, usage, finish, calls, err := ExecuteChatRequest(context.Background(), client, Config{Model: "test"}, "system", "user", ChatRequestConfig{})
	if err != nil || content != "hello" || usage == nil || usage.TotalTokens != 7 || finish != "stop" || len(calls) != 0 {
		t.Fatalf("compatibility reply = %q, %+v, %q, %+v, %v", content, usage, finish, calls, err)
	}
}

type compatStreamProvider struct {
	fakeTextProvider
	chunks <-chan whtypes.TextChunk
}

func (p *compatStreamProvider) Stream(context.Context, whtypes.TextRequest) (<-chan whtypes.TextChunk, error) {
	return p.chunks, nil
}

func TestExecuteChatStreamRequestCompatibility(t *testing.T) {
	t.Parallel()
	finishReason := whtypes.FinishReasonStop
	stream := make(chan whtypes.TextChunk, 1)
	stream <- whtypes.TextChunk{Text: "hello", FinishReason: &finishReason, Usage: &whtypes.Usage{TotalTokens: 7}}
	close(stream)
	client := &compatStreamProvider{chunks: stream}
	count := 0
	content, usage, finish, calls, emitted, err := ExecuteChatStreamRequest(context.Background(), client, Config{Model: "test"}, "system", "user", func(chunk ChatStreamChunk) error {
		count++
		if chunk.ContentDelta != "hello" {
			t.Fatalf("delta = %q", chunk.ContentDelta)
		}
		return nil
	}, ChatRequestConfig{})
	if err != nil || content != "hello" || usage == nil || usage.TotalTokens != 7 || finish != "stop" || len(calls) != 0 || !emitted || count != 1 {
		t.Fatalf("compatibility stream = %q, %+v, %q, %+v, %t, %v; callbacks=%d", content, usage, finish, calls, emitted, err, count)
	}
}

func TestFinalizeChatRequestFailureDiscardsReply(t *testing.T) {
	t.Parallel()
	cause := errors.New("failed")
	result, err := finalizeChatRequest(chatAttempt{backendName: "backend"}, time.Now(), chatReply{content: "partial", usage: &LLMUsage{TotalTokens: 7}, finishReason: "stop", toolCalls: []ToolCall{{ID: "call"}}}, cause)
	if !errors.Is(err, cause) || result.content != "" || result.usage != nil || result.finishReason != "" || result.toolCalls != nil {
		t.Fatalf("failed request result = %+v, %v", result, err)
	}
}
