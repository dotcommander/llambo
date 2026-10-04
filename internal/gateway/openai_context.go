package gateway

import (
	"context"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// openAIRequestContextTimeout attaches OpenAI-compatible request options to
// the handler-bounded context used by both buffered and streaming completions.
func openAIRequestContextTimeout(parent context.Context, req ChatCompletionRequest, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	ctx = providers.WithChatRequestOverrides(ctx, req.MaxTokens, req.Temperature, req.TopP)
	ctx = providers.WithJSONOverrides(ctx, chatJSONOverrides(req))
	ctx = providers.WithResponseFormatOverride(ctx, responseFormatOverride(req.ResponseFormat))
	return ctx, cancel
}
