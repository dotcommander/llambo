package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// handleChatCompletionStream handles streaming chat completions in OpenAI SSE format.
func (s *Server) handleChatCompletionStream(w http.ResponseWriter, r *http.Request, req ChatCompletionRequest) {
	_, ok := any(s.provider).(providers.ChatStreamProvider)
	if !ok {
		writeError(w, http.StatusNotImplemented, "not_implemented", "Streaming not supported by provider")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "Streaming unavailable")
		return
	}

	ctx, cancel := openAIRequestContextTimeout(r.Context(), req, s.effectiveHandlerTimeout())
	defer cancel()

	completionID := generateID("chatcmpl")
	created := time.Now().Unix()

	// Headers must be set before first Write
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// The provider selected by routing may differ from req.Model. Do not commit
	// either transparency header until an actual stream chunk or final result
	// identifies the backend that served this request.
	roleSent := false
	finishSent := false
	written := false
	setProviderHeaders := func(provider, model string) {
		if provider != "" {
			w.Header().Set("X-Llambo-Provider", provider)
		}
		if model != "" {
			w.Header().Set("X-Llambo-Model", model)
		}
	}
	emitAssistantRole := func() error {
		if roleSent {
			return nil
		}
		if err := writeOpenAISSEChunk(w, flusher, ChatCompletionResponse{
			ID:      completionID,
			Object:  ObjectChatCompletionChunk,
			Created: created,
			Choices: []Choice{{
				Index: 0,
				Delta: &Delta{Role: "assistant"},
			}},
		}); err != nil {
			return err
		}
		roleSent = true
		written = true
		return nil
	}

	result, err := s.executeChatStream(ctx, req, func(chunk providers.ChatStreamChunk) (callbackErr error) {
		defer func() {
			if callbackErr != nil {
				cancel()
			}
		}()
		setProviderHeaders(chunk.Provider, chunk.Model)
		if chunk.ContentDelta != "" || len(chunk.ToolDeltas) > 0 {
			tools := make([]OpenAIToolDelta, 0, len(chunk.ToolDeltas))
			for _, td := range chunk.ToolDeltas {
				tools = append(tools, OpenAIToolDelta{Index: td.Index, ID: td.ID, Type: "function", Function: OpenAIToolFunction{Name: td.Name, Arguments: td.ArgumentsDelta}})
			}
			if err := emitAssistantRole(); err != nil {
				return err
			}
			if err := writeOpenAISSEChunk(w, flusher, ChatCompletionResponse{
				ID:      completionID,
				Object:  ObjectChatCompletionChunk,
				Created: created,
				Choices: []Choice{{
					Index: 0,
					Delta: &Delta{Content: chunk.ContentDelta, ToolCalls: tools},
				}},
			}); err != nil {
				return err
			}
			written = true
		}

		if chunk.FinishReason != "" && !finishSent {
			if err := emitAssistantRole(); err != nil {
				return err
			}
			finishSent = true
			reason := normalizeOpenAIFinishReason(chunk.FinishReason)
			if err := writeOpenAISSEChunk(w, flusher, ChatCompletionResponse{
				ID:      completionID,
				Object:  ObjectChatCompletionChunk,
				Created: created,
				Choices: []Choice{{
					Index:        0,
					FinishReason: reason,
					Delta:        &Delta{},
				}},
			}); err != nil {
				return err
			}
			written = true
		}

		return nil
	})
	if err != nil {
		// If nothing was written yet, emit a proper HTTP error response.
		// Once any SSE event has been flushed, fall back to the SSE error
		// chunk (the OpenAI contract has no spec for mid-stream errors,
		// but clients commonly accept this shape).
		if !written {
			writeErrorDetail(w, http.StatusBadGateway, upstreamErrorDetail(err))
			return
		}
		errPayload := ErrorResponse{
			Error: upstreamErrorDetail(err),
		}
		b, _ := json.Marshal(errPayload)
		if _, writeErr := fmt.Fprintf(w, "data: %s\n\n", b); writeErr != nil {
			slog.Debug("sse error chunk write failed", "error", writeErr)
		} else {
			flusher.Flush()
		}
		writeOpenAISSEDone(w, flusher)
		return
	}

	if !written {
		setProviderHeaders(result.Provider, result.Model)
	}
	if !roleSent {
		_ = emitAssistantRole()
	}

	// Send finish_reason from result if not already sent via callback
	if !finishSent && result.FinishReason != "" {
		reason := normalizeOpenAIFinishReason(result.FinishReason)
		_ = writeOpenAISSEChunk(w, flusher, ChatCompletionResponse{
			ID:      completionID,
			Object:  ObjectChatCompletionChunk,
			Created: created,
			Model:   result.Model,
			Choices: []Choice{{
				Index:        0,
				FinishReason: reason,
				Delta:        &Delta{},
			}},
		})
	}

	// Usage and extension metadata are emitted only when requested.
	meta := &LlamboMeta{Backend: result.Provider, Model: result.Model}
	var usage *Usage
	if result.Usage != nil {
		usage = &Usage{PromptTokens: result.Usage.PromptTokens, CompletionTokens: result.Usage.CompletionTokens, TotalTokens: result.Usage.TotalTokens}
		if result.Usage.Cost != nil {
			meta.CostUSD = result.Usage.Cost.TotalCost
		}
	}
	if req.StreamOptions != nil && req.StreamOptions.IncludeUsage {
		_ = writeOpenAISSEChunk(w, flusher, ChatCompletionResponse{
			ID: completionID, Object: ObjectChatCompletionChunk, Created: created, Model: result.Model,
			Choices: []Choice{}, Usage: usage, XLlambo: meta,
		})
	}

	writeOpenAISSEDone(w, flusher)
}

func writeOpenAISSEChunk(w http.ResponseWriter, flusher http.Flusher, chunk ChatCompletionResponse) error {
	b, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeOpenAISSEDone(w http.ResponseWriter, flusher http.Flusher) {
	if _, err := fmt.Fprintf(w, "data: [DONE]\n\n"); err != nil {
		slog.Debug("sse done write failed", "error", err)
	}
	flusher.Flush()
}
