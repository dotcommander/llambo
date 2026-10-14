package providers

import (
	"errors"
	"fmt"
	"strings"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

// maxStreamToolIndex bounds tool-call indices accepted from the wire so a
// hostile chunk cannot force an unbounded accumulation buffer.
const maxStreamToolIndex = 4096

// toolIndexError aborts a stream whose tool-call indexing cannot be trusted;
// aggregated partial tool calls are dropped for that attempt.
type toolIndexError struct{ index int }

func (e toolIndexError) Error() string { return fmt.Sprintf("invalid tool index %d", e.index) }

// textStreamAccumulator folds wire text chunks into the aggregated stream
// result: content, indexed tool calls, finish reason, usage, and response
// identity. It owns chunk normalization; consumeTextStream owns the loop and
// the emission/cancellation fences.
type textStreamAccumulator struct {
	content      strings.Builder
	toolCalls    []ToolCall
	finishReason string
	usage        *LLMUsage
	emitted      bool
	identity     chatResponseIdentity
}

// absorb folds one wire chunk and returns the outbound chunk plus whether it
// carries any visible output worth emitting to the consumer.
func (a *textStreamAccumulator) absorb(chunk whtypes.TextChunk) (ChatStreamChunk, bool, error) {
	if chunk.Error != nil {
		return ChatStreamChunk{}, false, chunk.Error
	}
	a.observeIdentity(chunk)

	out := ChatStreamChunk{Provider: chunk.Provider, Model: chunk.Model}
	if delta := chunk.Content(); delta != "" {
		a.content.WriteString(delta)
		out.ContentDelta = delta
	}
	if err := a.absorbToolCalls(chunk, &out); err != nil {
		return ChatStreamChunk{}, false, err
	}
	if chunk.FinishReason != nil {
		a.finishReason = string(*chunk.FinishReason)
		out.FinishReason = a.finishReason
	}

	emit := out.ContentDelta != "" || len(out.ToolDeltas) > 0 || out.FinishReason != ""
	return out, emit, nil
}

// observeIdentity records the declaring provider/model and usage from a chunk.
func (a *textStreamAccumulator) observeIdentity(chunk whtypes.TextChunk) {
	if chunk.Provider != "" {
		a.identity.provider = chunk.Provider
	}
	if chunk.Model != "" {
		a.identity.model = chunk.Model
	}
	if chunk.Usage != nil {
		a.usage = usageFromWormhole(chunk.Usage)
	}
}

// wireToolCalls resolves a chunk's tool calls across the three wire shapes:
// batched ToolCalls, delta-attached calls, and a single ToolCall field.
func wireToolCalls(chunk whtypes.TextChunk) []whtypes.ToolCall {
	if len(chunk.ToolCalls) > 0 {
		return chunk.ToolCalls
	}
	if chunk.Delta != nil && len(chunk.Delta.ToolCalls) > 0 {
		return chunk.Delta.ToolCalls
	}
	if chunk.ToolCall != nil {
		return []whtypes.ToolCall{*chunk.ToolCall}
	}
	return nil
}

// absorbToolCalls merges one chunk's incremental tool calls into the
// accumulator by index, mirroring each delta onto the outbound chunk.
func (a *textStreamAccumulator) absorbToolCalls(chunk whtypes.TextChunk, out *ChatStreamChunk) error {
	for _, raw := range wireToolCalls(chunk) {
		tc := convertWormholeToolCalls([]whtypes.ToolCall{raw})[0]
		index := raw.Index
		if index < 0 || index > maxStreamToolIndex {
			return toolIndexError{index: index}
		}
		for len(a.toolCalls) <= index {
			a.toolCalls = append(a.toolCalls, ToolCall{})
		}
		current := &a.toolCalls[index]
		if tc.ID != "" {
			current.ID = tc.ID
		}
		if tc.Name != "" {
			current.Name = tc.Name
		}
		current.Arguments += tc.Arguments
		out.ToolDeltas = append(out.ToolDeltas, ToolCallDelta{Index: index, ID: tc.ID, Name: tc.Name, ArgumentsDelta: tc.Arguments})
	}
	return nil
}

// finalize closes the stream: content without any text or tool-call output is
// a failed generation.
func (a *textStreamAccumulator) finalize(model string) (string, error) {
	content := strings.TrimSpace(a.content.String())
	if content == "" && len(a.toolCalls) == 0 {
		return "", fmt.Errorf("%s: no content in streaming response", model)
	}
	return content, nil
}

// abortedToolCalls reports the aggregated tool calls for a failed stream:
// an untrustworthy tool index suppresses them, every other abort keeps the
// partial calls for diagnostics.
func (a *textStreamAccumulator) abortedToolCalls(err error) []ToolCall {
	var invalid toolIndexError
	if errors.As(err, &invalid) {
		return nil
	}
	return compactToolCalls(a.toolCalls)
}

// chatStreamOutcome bundles one streaming attempt's terminal state: the
// aggregated reply, whether any visible output reached the consumer, the
// declaring provider/model identity, and the terminal error.
type chatStreamOutcome struct {
	reply    chatReply
	emitted  bool
	identity chatResponseIdentity
	err      error
}

// consumeTextStream folds the wire chunk stream into one attempt's outcome.
// It owns the loop and the emission/cancellation fences over the accumulator.
func consumeTextStream(stream <-chan whtypes.TextChunk, model string, onChunk ChatStreamHandler) chatStreamOutcome {
	var acc textStreamAccumulator
	for chunk := range stream {
		out, emit, err := acc.absorb(chunk)
		if err != nil {
			return chatStreamOutcome{
				reply:    chatReply{usage: acc.usage, finishReason: acc.finishReason, toolCalls: acc.abortedToolCalls(err)},
				emitted:  acc.emitted,
				identity: acc.identity,
				err:      err,
			}
		}
		if !emit {
			continue
		}
		acc.emitted = true
		if onChunk != nil {
			if err := onChunk(out); err != nil {
				return chatStreamOutcome{
					reply:    chatReply{usage: acc.usage, finishReason: acc.finishReason, toolCalls: compactToolCalls(acc.toolCalls)},
					emitted:  acc.emitted,
					identity: acc.identity,
					err:      &ConsumerError{Err: err},
				}
			}
		}
	}

	content, err := acc.finalize(model)
	if err != nil {
		return chatStreamOutcome{
			reply:    chatReply{usage: acc.usage, finishReason: acc.finishReason},
			emitted:  acc.emitted,
			identity: acc.identity,
			err:      err,
		}
	}
	return chatStreamOutcome{
		reply:    chatReply{content: content, usage: acc.usage, finishReason: acc.finishReason, toolCalls: compactToolCalls(acc.toolCalls)},
		emitted:  acc.emitted,
		identity: acc.identity,
	}
}

func compactToolCalls(in []ToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(in))
	for _, tc := range in {
		if tc.ID == "" && tc.Name == "" && tc.Arguments == "" {
			continue
		}
		out = append(out, tc)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
