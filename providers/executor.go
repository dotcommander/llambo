package providers

import "context"

// ChatExecutor executes chat completion requests
type ChatExecutor interface {
	ExecuteChat(ctx context.Context, backendName string, systemPrompt, userContent string) (string, error)
}
