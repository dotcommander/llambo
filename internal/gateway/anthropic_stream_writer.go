package gateway

import (
	"net/http"

	"github.com/dotcommander/llambo/providers"
)

type anthropicStreamWriter struct {
	w                http.ResponseWriter
	flusher          http.Flusher
	state            *anthropicStreamState
	started, written bool
	pending          []providers.ChatStreamChunk
}

func (writer *anthropicStreamWriter) startMessage(provider, model string) error {
	if writer.started {
		return nil
	}
	if provider != "" {
		writer.w.Header().Set("X-Llambo-Provider", provider)
	}
	if model != "" {
		writer.w.Header().Set("X-Llambo-Model", model)
	}
	writer.state.model = model
	if err := writeAnthropicSSEEvent(writer.w, writer.flusher, "message_start", map[string]any{
		typeStr: "message_start",
		message1: map[string]any{
			"id":          writer.state.messageID,
			typeStr:       message1,
			"role":        assistant1,
			"content":     []any{},
			"model":       writer.state.model,
			"stop_reason": nil,
			stopSequence:  nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	}); err != nil {
		return err
	}
	writer.started = true
	writer.written = true
	return nil
}

func (writer *anthropicStreamWriter) processChunk(chunk providers.ChatStreamChunk) error {
	if chunk.ContentDelta != "" {
		if err := writer.state.processTextDelta(writer.w, writer.flusher, chunk.ContentDelta); err != nil {
			return err
		}
	}

	for _, td := range chunk.ToolDeltas {
		blockIdx, err := writer.state.ensureToolBlock(writer.w, writer.flusher, td)
		if err != nil {
			return err
		}
		if td.ArgumentsDelta != "" {
			if err := writeAnthropicSSEEvent(writer.w, writer.flusher, "content_block_delta", map[string]any{
				typeStr: "content_block_delta",
				index1:  blockIdx,
				delta1: map[string]any{
					typeStr:        "input_json_delta",
					"partial_json": td.ArgumentsDelta,
				},
			}); err != nil {
				return err
			}
		}
	}

	if chunk.FinishReason != "" {
		writer.state.finishReason = chunk.FinishReason
	}

	return nil
}

func (writer *anthropicStreamWriter) consume(chunk providers.ChatStreamChunk) error {
	if !writer.started {
		if chunk.Provider == "" && chunk.Model == "" {
			writer.pending = append(writer.pending, chunk)
			return nil
		}
		if err := writer.startMessage(chunk.Provider, chunk.Model); err != nil {
			return err
		}
		for _, buffered := range writer.pending {
			if err := writer.processChunk(buffered); err != nil {
				return err
			}
		}
		writer.pending = nil
	}
	return writer.processChunk(chunk)
}

func (writer *anthropicStreamWriter) fail(err error, requestID string) {
	if !writer.written {
		_, message := sanitizeUpstreamError(err)
		writeAnthropicError(writer.w, anthropicStatusFromError(err), requestID, message)
		return
	}
	_, message := sanitizeUpstreamError(err)
	_ = writeAnthropicSSEEvent(writer.w, writer.flusher, errorStr, map[string]any{
		typeStr: errorStr,
		errorStr: map[string]any{
			typeStr:  anthropicErrorTypeFromStatus(anthropicStatusFromError(err)),
			message1: message,
		},
		"request_id": requestID,
	})
}

func (writer *anthropicStreamWriter) finish(result providers.ChatResult) bool {
	if !writer.started {
		if err := writer.startMessage(result.Provider, result.Model); err != nil {
			return false
		}
		for _, buffered := range writer.pending {
			if err := writer.processChunk(buffered); err != nil {
				return false
			}
		}
	}
	if result.FinishReason == "" {
		result.FinishReason = writer.state.finishReason
	}
	if err := writer.state.flushPendingText(writer.w, writer.flusher); err != nil {
		return false
	}

	writer.state.stopAllBlocks(writer.w, writer.flusher)

	stopReason := mapOpenAIFinishReasonToAnthropic(result.FinishReason, writer.state.matchedStop)
	inputTokens := 0
	outputTokens := 0
	if result.Usage != nil {
		inputTokens = result.Usage.PromptTokens
		outputTokens = result.Usage.CompletionTokens
	}
	_ = writeAnthropicSSEEvent(writer.w, writer.flusher, "message_delta", map[string]any{
		typeStr: "message_delta",
		delta1: map[string]any{
			"stop_reason": stopReason,
			stopSequence:  writer.state.matchedStop,
		},
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
		"x_provider": result.Provider,
	})
	_ = writeAnthropicSSEEvent(writer.w, writer.flusher, "message_stop", map[string]any{typeStr: "message_stop"})
	return true
}
