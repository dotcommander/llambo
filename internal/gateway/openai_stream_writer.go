package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dotcommander/llambo/providers"
)

type openAIStreamWriter struct {
	w                             http.ResponseWriter
	flusher                       http.Flusher
	completionID                  string
	created                       int64
	roleSent, finishSent, written bool
}

func (state *openAIStreamWriter) setProviderHeaders(provider, model string) {
	if provider != "" {
		state.w.Header().Set("X-Llambo-Provider", provider)
	}
	if model != "" {
		state.w.Header().Set("X-Llambo-Model", model)
	}
}

func (state *openAIStreamWriter) emitAssistantRole() error {
	if state.roleSent {
		return nil
	}
	if err := writeOpenAISSEChunk(state.w, state.flusher, ChatCompletionResponse{
		ID:      state.completionID,
		Object:  ObjectChatCompletionChunk,
		Created: state.created,
		Choices: []Choice{{
			Index: 0,
			Delta: &Delta{Role: assistant1},
		}},
	}); err != nil {
		return err
	}
	state.roleSent = true
	state.written = true
	return nil
}

func (state *openAIStreamWriter) consume(chunk providers.ChatStreamChunk) error {
	state.setProviderHeaders(chunk.Provider, chunk.Model)
	if err := state.emitContent(chunk); err != nil {
		return err
	}
	return state.emitFinish(chunk)
}

func (state *openAIStreamWriter) emitContent(chunk providers.ChatStreamChunk) error {
	if chunk.ContentDelta == "" && len(chunk.ToolDeltas) == 0 {
		return nil
	}
	tools := make([]OpenAIToolDelta, 0, len(chunk.ToolDeltas))
	for _, td := range chunk.ToolDeltas {
		tools = append(tools, OpenAIToolDelta{Index: td.Index, ID: td.ID, Type: function1, Function: OpenAIToolFunction{Name: td.Name, Arguments: td.ArgumentsDelta}})
	}
	if err := state.emitAssistantRole(); err != nil {
		return err
	}
	if err := writeOpenAISSEChunk(state.w, state.flusher, ChatCompletionResponse{
		ID:      state.completionID,
		Object:  ObjectChatCompletionChunk,
		Created: state.created,
		Choices: []Choice{{
			Index: 0,
			Delta: &Delta{Content: chunk.ContentDelta, ToolCalls: tools},
		}},
	}); err != nil {
		return err
	}
	state.written = true
	return nil
}

func (state *openAIStreamWriter) emitFinish(chunk providers.ChatStreamChunk) error {
	if chunk.FinishReason == "" || state.finishSent {
		return nil
	}
	if err := state.emitAssistantRole(); err != nil {
		return err
	}
	state.finishSent = true
	reason := normalizeOpenAIFinishReason(chunk.FinishReason)
	if err := writeOpenAISSEChunk(state.w, state.flusher, ChatCompletionResponse{
		ID:      state.completionID,
		Object:  ObjectChatCompletionChunk,
		Created: state.created,
		Choices: []Choice{{
			Index:        0,
			FinishReason: reason,
			Delta:        &Delta{},
		}},
	}); err != nil {
		return err
	}
	state.written = true
	return nil
}

func (state *openAIStreamWriter) fail(ctx context.Context, err error) {
	// If nothing was state.written yet, emit a proper HTTP error response.
	// Once any SSE event has been flushed, fall back to the SSE error
	// chunk (the OpenAI contract has no spec for mid-stream errors,
	// but clients commonly accept this shape).
	if !state.written {
		writeErrorDetail(state.w, http.StatusBadGateway, upstreamErrorDetail(err))
		return
	}
	errPayload := ErrorResponse{
		Error: upstreamErrorDetail(err),
	}
	b, _ := json.Marshal(errPayload)
	if _, writeErr := fmt.Fprintf(state.w, "data: %s\n\n", b); writeErr != nil {
		wireLog().DebugContext(ctx, "sse error chunk write failed", errorStr, writeErr)
	} else {
		state.flusher.Flush()
	}
	writeOpenAISSEDone(state.w, state.flusher)
}

func (state *openAIStreamWriter) finish(result providers.ChatResult, includeUsage bool) {
	if !state.written {
		state.setProviderHeaders(result.Provider, result.Model)
	}
	if !state.roleSent {
		_ = state.emitAssistantRole()
	}

	// Send finish_reason from result if not already sent via callback
	if !state.finishSent && result.FinishReason != "" {
		reason := normalizeOpenAIFinishReason(result.FinishReason)
		_ = writeOpenAISSEChunk(state.w, state.flusher, ChatCompletionResponse{
			ID:      state.completionID,
			Object:  ObjectChatCompletionChunk,
			Created: state.created,
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
	if includeUsage {
		_ = writeOpenAISSEChunk(state.w, state.flusher, ChatCompletionResponse{
			ID: state.completionID, Object: ObjectChatCompletionChunk, Created: state.created, Model: result.Model,
			Choices: []Choice{}, Usage: usage, XLlambo: meta,
		})
	}

	writeOpenAISSEDone(state.w, state.flusher)
}
