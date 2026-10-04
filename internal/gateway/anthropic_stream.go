package gateway

import (
	"context"
	"net/http"

	"github.com/dotcommander/llambo/providers"
)

type anthropicStreamState struct {
	messageID     string
	model         string
	textStarted   bool
	textIndex     int
	nextIndex     int
	toolIndexMap  map[int]int
	openBlocks    []int
	finishReason  string
	stopSequences []string
	maxStopLen    int
	pendingText   string
	matchedStop   *string
}

func newAnthropicStreamState(model string, stopSequences []string) *anthropicStreamState {
	maxLen := 0
	for _, s := range stopSequences {
		if l := len(s); l > maxLen {
			maxLen = l
		}
	}
	return &anthropicStreamState{
		messageID:     anthropicID("msg"),
		model:         model,
		textIndex:     0,
		nextIndex:     0,
		toolIndexMap:  map[int]int{},
		openBlocks:    make([]int, 0, 4),
		stopSequences: stopSequences,
		maxStopLen:    maxLen,
	}
}

func (s *Server) handleAnthropicMessagesStream(w http.ResponseWriter, r *http.Request, parsed *anthropicParsedRequest) {
	req := parsed.Original
	_, ok := any(s.provider).(providers.ChatStreamProvider)
	if !ok {
		writeAnthropicError(w, http.StatusNotImplemented, parsed.RequestID, "Streaming not yet implemented")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicError(w, http.StatusInternalServerError, parsed.RequestID, "Streaming unavailable")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ctx, cancel := context.WithTimeout(r.Context(), s.effectiveHandlerTimeout())
	defer cancel()
	ctx = parsed.enrichContext(ctx)

	state := newAnthropicStreamState("", req.StopSequences)
	started := false
	written := false
	startMessage := func(provider, model string) error {
		if started {
			return nil
		}
		if provider != "" {
			w.Header().Set("X-Llambo-Provider", provider)
		}
		if model != "" {
			w.Header().Set("X-Llambo-Model", model)
		}
		state.model = model
		if err := writeAnthropicSSEEvent(w, flusher, "message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            state.messageID,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         state.model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		}); err != nil {
			return err
		}
		started = true
		written = true
		return nil
	}

	pending := make([]providers.ChatStreamChunk, 0, 2)
	processChunk := func(chunk providers.ChatStreamChunk) (callbackErr error) {
		defer func() {
			if callbackErr != nil {
				cancel()
			}
		}()
		if chunk.ContentDelta != "" {
			if err := state.processTextDelta(w, flusher, chunk.ContentDelta); err != nil {
				return err
			}
		}

		for _, td := range chunk.ToolDeltas {
			blockIdx, err := state.ensureToolBlock(w, flusher, td)
			if err != nil {
				return err
			}
			if td.ArgumentsDelta != "" {
				if err := writeAnthropicSSEEvent(w, flusher, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": blockIdx,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": td.ArgumentsDelta,
					},
				}); err != nil {
					return err
				}
			}
		}

		if chunk.FinishReason != "" {
			state.finishReason = chunk.FinishReason
		}

		return nil
	}
	result, err := s.executeChatStream(ctx, parsed.Translated, func(chunk providers.ChatStreamChunk) (callbackErr error) {
		defer func() {
			if callbackErr != nil {
				cancel()
			}
		}()
		if !started {
			if chunk.Provider == "" && chunk.Model == "" {
				pending = append(pending, chunk)
				return nil
			}
			if err := startMessage(chunk.Provider, chunk.Model); err != nil {
				return err
			}
			for _, buffered := range pending {
				if err := processChunk(buffered); err != nil {
					return err
				}
			}
			pending = nil
		}
		return processChunk(chunk)
	})
	if err != nil {
		if !written {
			_, message := sanitizeUpstreamError(err)
			writeAnthropicError(w, anthropicStatusFromError(err), parsed.RequestID, message)
			return
		}
		_, message := sanitizeUpstreamError(err)
		_ = writeAnthropicSSEEvent(w, flusher, "error", map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    anthropicErrorTypeFromStatus(anthropicStatusFromError(err)),
				"message": message,
			},
			"request_id": parsed.RequestID,
		})
		return
	}

	if !started {
		if err := startMessage(result.Provider, result.Model); err != nil {
			return
		}
		for _, buffered := range pending {
			if err := processChunk(buffered); err != nil {
				return
			}
		}
	}
	if result.FinishReason == "" {
		result.FinishReason = state.finishReason
	}
	if err := state.flushPendingText(w, flusher); err != nil {
		return
	}

	state.stopAllBlocks(w, flusher)

	stopReason := mapOpenAIFinishReasonToAnthropic(result.FinishReason, state.matchedStop)
	inputTokens := 0
	outputTokens := 0
	if result.Usage != nil {
		inputTokens = result.Usage.PromptTokens
		outputTokens = result.Usage.CompletionTokens
	}
	_ = writeAnthropicSSEEvent(w, flusher, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": state.matchedStop,
		},
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
		"x_provider": result.Provider,
	})
	_ = writeAnthropicSSEEvent(w, flusher, "message_stop", map[string]any{"type": "message_stop"})
}
