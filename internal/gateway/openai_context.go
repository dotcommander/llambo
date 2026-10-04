package gateway

import (
	"context"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// openAIRequestContext attaches OpenAI-compatible request options to the
// handler-bounded context used by both buffered and streaming completions.
func openAIRequestContext(parent context.Context, req ChatCompletionRequest) (context.Context, context.CancelFunc) {
	return openAIRequestContextTimeout(parent, req, HandlerTimeout)
}
func openAIRequestContextTimeout(parent context.Context, req ChatCompletionRequest, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	ctx = providers.WithChatRequestOverrides(ctx, req.MaxTokens, req.Temperature, req.TopP)
	ctx = providers.WithJSONOverrides(ctx, chatJSONOverrides(req))
	ctx = providers.WithResponseFormatOverride(ctx, responseFormatOverride(req.ResponseFormat))
	return ctx, cancel
}
