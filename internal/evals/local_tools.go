package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const localAdapterResponseLimit = 2 << 20

// ToolChatClient is the deliberately narrow boundary for tool-use evaluation.
// Production wiring owns protocol translation; this adapter owns only the
// evaluation contract and rejects ambiguous served identities.
type ToolChatClient interface {
	ToolChat(context.Context, ToolChatRequest) (ToolChatResponse, error)
}

type ToolChatRequest struct {
	Model        string            `json:"model"`
	Messages     []ToolChatMessage `json:"messages"`
	Tools        []ToolDefinition  `json:"tools"`
	RequiredTool string            `json:"required_tool,omitempty"`
}

type ToolChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ToolChatUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	Known        bool
}

type ToolChatResponse struct {
	Provider  string        `json:"provider"`
	Model     string        `json:"model"`
	ToolCalls []ToolCall    `json:"tool_calls"`
	Usage     ToolChatUsage `json:"usage,omitzero"`
}

// ToolEvaluationRequest contains the sealed fixture plus its request shape.
// VerifiedServedModel is accepted only when a caller has already completed an
// exact-model preflight; it prevents a protocol that omits model metadata from
// producing an unidentifiable receipt.
type ToolEvaluationRequest struct {
	Case                ToolCase
	Chat                ToolChatRequest
	VerifiedServedModel string
}

func EvaluateToolCase(ctx context.Context, client ToolChatClient, request ToolEvaluationRequest) (LocalCall, bool, error) {
	if client == nil {
		return LocalCall{}, false, fmt.Errorf("tool chat client is required")
	}
	if strings.TrimSpace(request.Case.ID) == "" || strings.TrimSpace(request.Chat.Model) == "" {
		return LocalCall{}, false, fmt.Errorf("tool case id and requested model are required")
	}
	for i, tool := range request.Chat.Tools {
		if strings.TrimSpace(tool.Name) == "" || !json.Valid(tool.Parameters) {
			return LocalCall{}, false, fmt.Errorf("tool definition %d requires name and JSON parameters", i+1)
		}
	}
	response, err := client.ToolChat(ctx, request.Chat)
	if err != nil {
		return LocalCall{}, false, fmt.Errorf("tool chat: %w", err)
	}
	served, err := localServedIdentity(request.Chat.Model, response.Provider, response.Model, request.VerifiedServedModel)
	if err != nil {
		return LocalCall{}, false, err
	}
	output, err := json.Marshal(struct {
		Calls []ToolCall `json:"calls"`
	}{Calls: response.ToolCalls})
	if err != nil {
		return LocalCall{}, false, fmt.Errorf("encode tool response: %w", err)
	}
	input, err := json.Marshal(request.Case)
	if err != nil {
		return LocalCall{}, false, fmt.Errorf("encode tool case: %w", err)
	}
	caseForScore := request.Case
	caseForScore.Calls = response.ToolCalls
	passed, err := ScoreToolCase(caseForScore)
	if err != nil {
		return LocalCall{}, false, err
	}
	return LocalCall{CaseID: request.Case.ID, RequestedModel: request.Chat.Model, ServedModel: served, Input: input, Output: output, InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens, UsageKnown: response.Usage.Known}, passed, nil
}

// localServedIdentity turns separately reported provider/model fields into the
// receipt identity. It never accepts a response without identity unless the
// caller provides an exact-model preflight result.
func localServedIdentity(requested, provider, model, verified string) (string, error) {
	provider, model, verified = strings.TrimSpace(provider), strings.TrimSpace(model), strings.TrimSpace(verified)
	served := ""
	if model != "" {
		if provider != "" {
			// Provider model IDs may themselves contain slashes (for example
			// groq/compound). A separately supplied provider therefore wins.
			served = provider + "/" + model
		} else if _, _, ok := strings.Cut(model, "/"); ok {
			served = model
		} else {
			return "", fmt.Errorf("execution did not report a provider for model %q", model)
		}
	}
	if served == "" {
		served = verified
	} else if verified != "" && served != verified {
		return "", fmt.Errorf("response served model %q differs from verified preflight %q", served, verified)
	}
	tracker := NewIdentityTracker(requested)
	if err := tracker.Record(served); err != nil {
		return "", err
	}
	return served, nil
}

// localProviderModel removes only the configured provider prefix before a
// direct provider endpoint call. The remaining exact ID may itself contain
// slashes, so SplitN/Cut is intentionally used just once.
func localProviderModel(requested, provider string) (string, error) {
	actualProvider, model, ok := strings.Cut(strings.TrimSpace(requested), "/")
	if !ok || actualProvider != provider || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("requested model %q must use %s/<exact-model-id>", requested, provider)
	}
	return model, nil
}
