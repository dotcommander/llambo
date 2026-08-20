package cmd

import (
	"fmt"
	"strings"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

type pingStreamStats struct {
	Content      string
	TokensIn     int
	TokensOut    int
	TTFB         time.Duration
	Generation   time.Duration
	OutputChunks int
}

func consumePingStream(stream <-chan whtypes.TextChunk, started time.Time) (pingStreamStats, error) {
	if started.IsZero() {
		started = time.Now()
	}

	var stats pingStreamStats
	var content strings.Builder
	var firstOutput time.Time
	var lastOutput time.Time

	for chunk := range stream {
		now := time.Now()
		if chunk.Error != nil {
			return stats, chunk.Error
		}

		if chunk.Usage != nil && !chunk.Usage.IsZero() {
			stats.TokensIn = chunk.Usage.PromptTokens
			stats.TokensOut = chunk.Usage.CompletionTokens
		}

		if delta := chunk.Content(); delta != "" {
			content.WriteString(delta)
		}
		hasOutput := pingStreamChunkHasOutput(chunk)
		if hasOutput && firstOutput.IsZero() {
			firstOutput = now
		}
		if hasOutput {
			stats.OutputChunks++
			lastOutput = now
		}
	}

	if firstOutput.IsZero() {
		return stats, fmt.Errorf("stream ended without generated output")
	}

	stats.Content = strings.TrimSpace(content.String())
	stats.TTFB = firstOutput.Sub(started)
	// A provider that buffers the entire response into one stream chunk does
	// not expose enough timing information for a generation-rate measurement.
	// Do not turn the final usage/close delay into fake tok/s.
	if stats.OutputChunks >= 2 {
		stats.Generation = lastOutput.Sub(firstOutput)
		if stats.Generation < time.Millisecond {
			stats.Generation = 0
		}
	}
	return stats, nil
}

func pingStreamChunkHasOutput(chunk whtypes.TextChunk) bool {
	if chunk.Content() != "" || chunk.Refusal != "" || chunk.Thinking != nil {
		return true
	}
	return chunk.ToolCall != nil || len(chunk.ToolCalls) > 0
}

func pingSpeedTokensPerSecond(tokens int, generation time.Duration) float64 {
	if tokens <= 0 || generation <= 0 {
		return 0
	}
	return float64(tokens) / generation.Seconds()
}

func pingShouldFallbackToNonStreaming(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "stream ended without generated output") ||
		strings.Contains(lower, "stream not implemented") ||
		strings.Contains(lower, "does not support stream") ||
		strings.Contains(lower, "streaming not supported")
}
