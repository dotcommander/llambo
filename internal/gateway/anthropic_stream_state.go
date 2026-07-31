package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"unicode/utf8"

	"github.com/dotcommander/llambo/providers"
)

func (s *anthropicStreamState) startTextBlock(w http.ResponseWriter, flusher http.Flusher) error {
	if s.textStarted {
		return nil
	}
	s.textStarted = true
	if s.nextIndex == 0 {
		s.nextIndex = 1
	}
	s.openBlocks = append(s.openBlocks, s.textIndex)
	return writeAnthropicSSEEvent(w, flusher, "content_block_start", map[string]any{
		"type": "content_block_start", "index": s.textIndex,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
}

func (s *anthropicStreamState) ensureToolBlock(w http.ResponseWriter, flusher http.Flusher, delta providers.ToolCallDelta) (int, error) {
	if idx, ok := s.toolIndexMap[delta.Index]; ok {
		return idx, nil
	}
	idx := s.nextIndex
	s.nextIndex++
	s.toolIndexMap[delta.Index] = idx
	s.openBlocks = append(s.openBlocks, idx)
	id := delta.ID
	if id == "" {
		id = fmt.Sprintf("toolu_%d", delta.Index)
	}
	name := delta.Name
	if name == "" {
		name = fmt.Sprintf("tool_%d", delta.Index)
	}
	err := writeAnthropicSSEEvent(w, flusher, "content_block_start", map[string]any{
		"type": "content_block_start", "index": idx,
		"content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}},
	})
	return idx, err
}

func (s *anthropicStreamState) stopAllBlocks(w http.ResponseWriter, flusher http.Flusher) {
	indexes := append([]int(nil), s.openBlocks...)
	sort.Ints(indexes)
	for _, idx := range indexes {
		_ = writeAnthropicSSEEvent(w, flusher, "content_block_stop", map[string]any{
			"type": "content_block_stop", "index": idx,
		})
	}
}

func (s *anthropicStreamState) processTextDelta(w http.ResponseWriter, flusher http.Flusher, delta string) error {
	if delta == "" || s.matchedStop != nil {
		return nil
	}
	if len(s.stopSequences) == 0 {
		return s.emitTextDelta(w, flusher, delta)
	}
	s.pendingText += delta
	if matched, matchIdx := matchStopSequence(s.pendingText, s.stopSequences); matched != nil {
		if matchIdx > 0 {
			if err := s.emitTextDelta(w, flusher, s.pendingText[:matchIdx]); err != nil {
				return err
			}
		}
		s.matchedStop = matched
		s.pendingText = ""
		return nil
	}
	keep := max(s.maxStopLen-1, 0)
	if len(s.pendingText) > keep {
		cut := utf8SafePrefixLen(s.pendingText, len(s.pendingText)-keep)
		if cut == 0 {
			return nil
		}
		emit := s.pendingText[:cut]
		s.pendingText = s.pendingText[cut:]
		if emit != "" {
			return s.emitTextDelta(w, flusher, emit)
		}
	}
	return nil
}

// utf8SafePrefixLen returns a valid UTF-8 prefix no longer than limit. Stream
// transports may split a rune across deltas; retaining the incomplete suffix
// keeps emitted Anthropic text valid while stop matching continues on bytes.
func utf8SafePrefixLen(s string, limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > len(s) {
		limit = len(s)
	}
	for limit > 0 && !utf8.ValidString(s[:limit]) {
		limit--
	}
	return limit
}

func (s *anthropicStreamState) flushPendingText(w http.ResponseWriter, flusher http.Flusher) error {
	if s.pendingText == "" || s.matchedStop != nil {
		return nil
	}
	emit := s.pendingText
	s.pendingText = ""
	return s.emitTextDelta(w, flusher, emit)
}

func (s *anthropicStreamState) emitTextDelta(w http.ResponseWriter, flusher http.Flusher, text string) error {
	if text == "" {
		return nil
	}
	if err := s.startTextBlock(w, flusher); err != nil {
		return err
	}
	return writeAnthropicSSEEvent(w, flusher, "content_block_delta", map[string]any{
		"type": "content_block_delta", "index": s.textIndex,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
}

func writeAnthropicSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
