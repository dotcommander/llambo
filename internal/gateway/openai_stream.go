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
// wireLog returns a scoped logger bound to the current default handler, so
// tests that replace slog.SetDefault stay observable. Package-level slog
// calls are banned in functions that hold a request context.
func wireLog() *slog.Logger { return slog.New(slog.Default().Handler()) }

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

	state := openAIStreamWriter{w: w, flusher: flusher, completionID: generateID("chatcmpl"), created: time.Now().Unix()}
	setSSEHeaders(w)
	result, err := s.executeChatStream(ctx, req, func(chunk providers.ChatStreamChunk) error {
		if err := state.consume(chunk); err != nil {
			cancel()
			return err
		}
		return nil
	})
	if err != nil {
		state.fail(r.Context(), err)
		return
	}
	state.finish(result, req.StreamOptions != nil && req.StreamOptions.IncludeUsage)
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
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
		wireLog().Debug("sse done write failed", errorStr, err)
	}
	flusher.Flush()
}
