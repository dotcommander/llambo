package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/dotcommander/llambo/providers"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

// openAIRequestContextTimeout attaches OpenAI-compatible request options to
// the handler-bounded context used by both buffered and streaming completions.
func openAIRequestContextTimeout(parent context.Context, req ChatCompletionRequest, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	ctx = providers.WithChatRequestOverrides(ctx, req.MaxTokens, req.Temperature, req.TopP)
	ctx = providers.WithJSONOverrides(ctx, chatJSONOverrides(req))
	ctx = providers.WithResponseFormatOverride(ctx, responseFormatOverride(req.ResponseFormat))
	ctx = providers.WithRequestReasoning(ctx, req.reasoning)
	return ctx, cancel
}

// reasoningOverride translates OpenAI-compatible reasoning controls onto
// wormhole's provider-neutral reasoning request field. reasoning_effort is the
// chat-completions string form; reasoning is the richer object form and wins
// when both are present. An explicit reasoning object that is not JSON is a
// request error.
func reasoningOverride(req ChatCompletionRequest) (*whtypes.Reasoning, error) {
	var effortOnly *whtypes.Reasoning
	if trimmed := strings.TrimSpace(req.ReasoningEffort); trimmed != "" {
		effortOnly = &whtypes.Reasoning{Effort: whtypes.ReasoningEffort(trimmed)}
	}
	if len(req.Reasoning) == 0 || isRawNull(req.Reasoning) {
		return effortOnly, nil
	}
	var parsed whtypes.Reasoning
	if err := json.Unmarshal(req.Reasoning, &parsed); err != nil {
		return nil, errors.New("reasoning must be an object")
	}
	if parsed.Effort == "" && effortOnly != nil {
		parsed.Effort = effortOnly.Effort
	}
	return &parsed, nil
}
