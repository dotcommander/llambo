package providers

import "context"

// ChatExecutor executes chat completion requests
type ChatExecutor interface {
	ExecuteChat(ctx context.Context, backendName string, systemPrompt, userContent string) (string, error)
}

// StructuredChatExecutor is the additive ordered-request executor contract.
type StructuredChatExecutor interface {
	ExecuteStructuredChat(ctx context.Context, backendName string, cfg Config, request StructuredChatRequest) (ChatResult, error)
}
