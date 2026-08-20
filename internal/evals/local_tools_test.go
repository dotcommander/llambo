package evals

import (
	"context"
	"encoding/json"
	"testing"
)

type toolClientFunc func(context.Context, ToolChatRequest) (ToolChatResponse, error)

func (f toolClientFunc) ToolChat(ctx context.Context, request ToolChatRequest) (ToolChatResponse, error) {
	return f(ctx, request)
}

func TestEvaluateToolCaseRecordsExactIdentityAndCanonicalArguments(t *testing.T) {
	t.Parallel()
	expected := ToolCall{Name: "weather", Arguments: json.RawMessage(`{"city":"Richmond","units":"f"}`)}
	client := toolClientFunc(func(ctx context.Context, request ToolChatRequest) (ToolChatResponse, error) {
		if request.Model != "groq/groq/compound" || ctx.Value("trace") != "sealed" {
			t.Fatal("request/context was not propagated")
		}
		return ToolChatResponse{Provider: "groq", Model: "groq/compound", ToolCalls: []ToolCall{{Name: "weather", Arguments: json.RawMessage(`{"units":"f","city":"Richmond"}`)}}, Usage: ToolChatUsage{InputTokens: 3, OutputTokens: 2}}, nil
	})
	call, pass, err := EvaluateToolCase(context.WithValue(context.Background(), "trace", "sealed"), client, ToolEvaluationRequest{Case: ToolCase{ID: "required", Expected: &expected}, Chat: ToolChatRequest{Model: "groq/groq/compound"}})
	if err != nil || !pass {
		t.Fatalf("EvaluateToolCase = pass %v, err %v", pass, err)
	}
	if call.ServedModel != "groq/groq/compound" || call.InputTokens != 3 || call.OutputTokens != 2 {
		t.Fatalf("unexpected call: %#v", call)
	}
}

func TestEvaluateToolCaseRejectsMissingOrUnstableIdentity(t *testing.T) {
	t.Parallel()
	client := toolClientFunc(func(context.Context, ToolChatRequest) (ToolChatResponse, error) {
		return ToolChatResponse{ToolCalls: nil}, nil
	})
	_, _, err := EvaluateToolCase(context.Background(), client, ToolEvaluationRequest{Case: ToolCase{ID: "none"}, Chat: ToolChatRequest{Model: "openrouter/@preset/free"}})
	if err == nil {
		t.Fatal("missing identity accepted")
	}
	if _, err := localServedIdentity("openrouter/@preset/free", "openrouter", "one", "openrouter/two"); err == nil {
		t.Fatal("preflight mismatch accepted")
	}
}
