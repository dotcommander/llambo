package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func TestPingSpeedTokensPerSecondUsesGenerationDuration(t *testing.T) {
	if got, want := pingSpeedTokensPerSecond(25, 2*time.Second), 12.5; got != want {
		t.Fatalf("speed = %v, want %v", got, want)
	}
	if got := pingSpeedTokensPerSecond(25, 0); got != 0 {
		t.Fatalf("speed with no generation duration = %v, want 0", got)
	}
}

func TestPingMaxTokensLeavesReasoningHeadroom(t *testing.T) {
	if got, want := pingMaxTokens(0), 256; got != want {
		t.Fatalf("default ping max tokens = %d, want %d", got, want)
	}
	if got, want := pingMaxTokens(4096), 256; got != want {
		t.Fatalf("capped ping max tokens = %d, want %d", got, want)
	}
	if got, want := pingMaxTokens(128), 128; got != want {
		t.Fatalf("configured ping max tokens = %d, want %d", got, want)
	}
}

func TestConsumePingStreamCapturesTTFBGenerationAndUsage(t *testing.T) {
	stream := make(chan whtypes.TextChunk)
	started := time.Now()
	go func() {
		defer close(stream)
		time.Sleep(10 * time.Millisecond)
		stream <- whtypes.TextChunk{Text: "hello"}
		time.Sleep(10 * time.Millisecond)
		stream <- whtypes.TextChunk{Text: " world"}
		stream <- whtypes.TextChunk{Usage: &whtypes.Usage{PromptTokens: 4, CompletionTokens: 5, TotalTokens: 9}}
	}()

	got, err := consumePingStream(stream, started)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "hello world" {
		t.Fatalf("content = %q, want %q", got.Content, "hello world")
	}
	if got.TokensIn != 4 || got.TokensOut != 5 {
		t.Fatalf("usage = %d/%d, want 4/5", got.TokensIn, got.TokensOut)
	}
	if got.TTFB < 5*time.Millisecond {
		t.Fatalf("TTFB = %s, want at least 5ms", got.TTFB)
	}
	if got.Generation < 5*time.Millisecond {
		t.Fatalf("generation duration = %s, want at least 5ms", got.Generation)
	}
}

func TestConsumePingStreamDoesNotInventSpeedForBufferedResponse(t *testing.T) {
	stream := make(chan whtypes.TextChunk, 2)
	stream <- whtypes.TextChunk{Text: "all at once", Usage: &whtypes.Usage{CompletionTokens: 100}}
	close(stream)

	got, err := consumePingStream(stream, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 0 {
		t.Fatalf("generation duration = %s, want 0 for one buffered output chunk", got.Generation)
	}
	if gotSpeed := pingSpeedTokensPerSecond(got.TokensOut, got.Generation); gotSpeed != 0 {
		t.Fatalf("speed = %v, want 0 for one buffered output chunk", gotSpeed)
	}
}

func TestPingShouldFallbackToNonStreamingForEmptyStream(t *testing.T) {
	if !pingShouldFallbackToNonStreaming(fmt.Errorf("stream ended without generated output")) {
		t.Fatal("expected empty stream to use non-streaming fallback")
	}
	if !pingShouldFallbackToNonStreaming(fmt.Errorf("PROVIDER_ERROR: provider does not support Stream")) {
		t.Fatal("expected unsupported stream to use non-streaming fallback")
	}
	if !pingShouldFallbackToNonStreaming(fmt.Errorf("streaming not supported")) {
		t.Fatal("expected streaming-not-supported error to use non-streaming fallback")
	}
	if pingShouldFallbackToNonStreaming(fmt.Errorf("context deadline exceeded")) {
		t.Fatal("unexpected fallback for a timeout")
	}
}

func TestPingResultJSONUsesMillisecondTimingUnits(t *testing.T) {
	data, err := json.Marshal(PingResult{
		Latency:       1500 * time.Millisecond,
		TTFB:          400 * time.Millisecond,
		Generation:    1100 * time.Millisecond,
		SpeedTokensPS: 12.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{`"latency_ms":1500`, `"ttfb_ms":400`, `"generation_duration_ms":1100`, `"speed_tokens_per_second":12.5`} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON %s missing %q", got, want)
		}
	}
}
