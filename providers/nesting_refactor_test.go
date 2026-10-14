package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

func TestCanaryAggregationFiltersBeforeChoosingBaseline(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	events := []RouteEvent{
		{Timestamp: start, ChosenProvider: "trial", IsCanary: true, Success: true, LatencyMs: 40, CostUSD: 2},
		{Timestamp: start, ChosenProvider: "trial", IsCanary: true, LatencyMs: 60, CostUSD: 4},
		{Timestamp: start, ChosenProvider: "control", Success: true, LatencyMs: 30, CostUSD: 1},
		{Timestamp: start.Add(-time.Second), ChosenProvider: "old"},
		{Timestamp: start.Add(-time.Second), ChosenProvider: "old"},
		{Timestamp: start, ChosenProvider: "incomplete", PromotionIneligible: true},
		{Timestamp: start, ChosenProvider: "incomplete", PromotionIneligible: true},
	}
	status := EvaluateCanary(&CanaryConfig{Provider: "trial", StartedAt: start.Format(time.RFC3339)}, events, nil)
	if status.BaselineProvider != "control" || status.CanaryRequests != 2 || status.BaselineRequests != 1 {
		t.Fatalf("aggregation = %+v", status)
	}
	if status.CanarySuccessRate != 0.5 || status.CanaryAvgLatencyMs != 50 || status.CanaryAvgCostUSD != 3 {
		t.Fatalf("trial averages = %+v", status)
	}
	if status.BaselineSuccessRate != 1 || status.BaselineAvgLatencyMs != 30 || status.BaselineAvgCostUSD != 1 {
		t.Fatalf("control averages = %+v", status)
	}
}

func TestCloneStructuredToolsOwnsBothSchemas(t *testing.T) {
	t.Parallel()
	tool := whtypes.NewTool("tool", "description", map[string]any{"properties": map[string]any{"input": "original"}})
	tool.InputSchema = map[string]any{"properties": map[string]any{"input": "original"}}
	tools := []whtypes.Tool{*tool}
	cloned := cloneStructuredTools(tools)
	cloned[0].InputSchema["properties"].(map[string]any)["input"] = "changed"
	cloned[0].Function.Parameters["properties"].(map[string]any)["input"] = "changed"
	if tools[0].InputSchema["properties"].(map[string]any)["input"] != "original" || tools[0].Function.Parameters["properties"].(map[string]any)["input"] != "original" {
		t.Fatal("cloned schemas alias the request")
	}
}

func TestStructuredSamplingPrecedesContextOverrides(t *testing.T) {
	t.Parallel()
	contextTokens, structuredTokens := 10, 20
	contextTemperature, structuredTemperature := 0.4, 0.0
	contextTopP, structuredTopP := 0.5, 0.0
	ctx := WithChatRequestOverrides(context.Background(), &contextTokens, &contextTemperature, &contextTopP)
	ctx = context.WithValue(ctx, structuredRequestKey{}, StructuredChatRequest{
		MaxTokens: &structuredTokens, Temperature: &structuredTemperature, TopP: &structuredTopP,
	})
	request := buildTextRequest(ctx, Config{Model: "test", MaxTokens: 30, Temperature: 0.6}, "system", "user")
	if request.MaxTokens == nil || *request.MaxTokens != structuredTokens || request.Temperature == nil || *request.Temperature != 0 || request.TopP == nil || *request.TopP != 0 {
		t.Fatalf("structured overrides = %+v", request)
	}
}

func TestCircuitBreakerRepeatedDisablePreservesTransition(t *testing.T) {
	t.Parallel()
	events := make([]CircuitBreakerEvent, 0, 1)
	breaker := NewCircuitBreaker([]string{"backend"}, func(event CircuitBreakerEvent) { events = append(events, event) })
	breaker.RecordFailure("backend", errors.New("rate limit exceeded"))
	first := breaker.GetHealth("backend")
	breaker.RecordFailure("backend", errors.New("rate limit exceeded"))
	second := breaker.GetHealth("backend")
	if len(events) != 1 || !events[0].IsQuota || !second.Disabled || second.Failures != first.Failures+1 {
		t.Fatalf("disable transitions = %+v, %+v", events, second)
	}
	if !second.DisabledAt.Equal(first.DisabledAt) || !second.CooldownUntil.Equal(first.CooldownUntil) {
		t.Fatalf("repeated failure reset quarantine: first=%+v second=%+v", first, second)
	}
}
