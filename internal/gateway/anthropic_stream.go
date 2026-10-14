package gateway

import (
	"context"
	"net/http"

	"github.com/dotcommander/llambo/providers"
)

// Repeated string literals consolidated by goconst.
const (
	delta1       = "delta"
	errorStr     = "error"
	message1     = "message"
	stopSequence = "stop_sequence"
	typeStr      = "type"
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

	setSSEHeaders(w)
	ctx, cancel := context.WithTimeout(r.Context(), s.effectiveHandlerTimeout())
	defer cancel()
	ctx = parsed.enrichContext(ctx)

	writer := anthropicStreamWriter{w: w, flusher: flusher, state: newAnthropicStreamState("", req.StopSequences), pending: make([]providers.ChatStreamChunk, 0, 2)}
	result, err := s.executeChatStream(ctx, parsed.Translated, func(chunk providers.ChatStreamChunk) error {
		if err := writer.consume(chunk); err != nil {
			cancel()
			return err
		}
		return nil
	})
	if err != nil {
		writer.fail(err, parsed.RequestID)
		return
	}
	if !writer.finish(result) {
		cancel()
	}
}
