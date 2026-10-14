package providers

import (
	"context"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

// ExecuteChatRequest sends a chat request with llambo-owned retries.
// Its scalar arguments and results are retained for existing callers.
//
//nolint:revive // Preserve the published argument and result contract; internals use bundles.
func ExecuteChatRequest(ctx context.Context, client whtypes.Provider, cfg Config, systemPrompt, userContent string, reqConfig ChatRequestConfig) (string, *LLMUsage, string, []ToolCall, error) {
	reply, _, err := executeChatRequestWithIdentity(ctx, client, chatAttempt{cfg: cfg, systemPrompt: systemPrompt, userContent: userContent}, reqConfig)
	return reply.content, reply.usage, reply.finishReason, reply.toolCalls, err
}

// ExecuteChatStreamRequest sends a streaming chat request and forwards deltas.
// Its scalar arguments and results are retained for existing callers.
//
//nolint:revive // Preserve the published argument and result contract; internals use bundles.
func ExecuteChatStreamRequest(ctx context.Context, client whtypes.Provider, cfg Config, systemPrompt, userContent string, onChunk ChatStreamHandler, reqConfig ChatRequestConfig) (string, *LLMUsage, string, []ToolCall, bool, error) {
	reply, emitted, _, err := executeChatStreamRequestWithIdentity(ctx, client, chatAttempt{cfg: cfg, systemPrompt: systemPrompt, userContent: userContent}, onChunk, reqConfig)
	return reply.content, reply.usage, reply.finishReason, reply.toolCalls, emitted, err
}
