package providers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPerformCanaryAutoPromotePersistsRawConfig(t *testing.T) {
	setTestConfigPath(t)
	const envOnlyKey = "env-only-auto-promote-secret"
	t.Setenv("LLAMBO_AUTO_PROMOTE_TEST_API_KEY", envOnlyKey)

	startedAt := time.Now().UTC().Add(-time.Minute)
	eventsPath := filepath.Join(t.TempDir(), "route-events.jsonl")
	var events []RouteEvent
	for range 5 {
		events = append(events,
			RouteEvent{Timestamp: startedAt.Add(time.Second), ChosenProvider: "canary", IsCanary: true, Success: true, LatencyMs: 50, CostUSD: 0.01},
			RouteEvent{Timestamp: startedAt.Add(time.Second), ChosenProvider: "baseline", Success: true, LatencyMs: 100, CostUSD: 0.02},
		)
	}
	var lines []byte
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal route event: %v", err)
		}
		lines = append(lines, line...)
		lines = append(lines, '\n')
	}
	if err := os.WriteFile(eventsPath, lines, 0o600); err != nil {
		t.Fatalf("write route events: %v", err)
	}

	writeTestConfig(t, `{
  "default_provider": "baseline",
  "providers": {
    "baseline": {"base_url":"https://baseline.example","model":"baseline","enabled":true,"priority":7,"requires_key":false,"api_key":"configured-baseline-key"},
    "canary": {"base_url":"https://canary.example","model":"canary","enabled":true,"priority":19,"env_var":"LLAMBO_AUTO_PROMOTE_TEST_API_KEY"}
  },
  "routing": {"canary":{"provider":"canary","traffic_pct":0.25,"promote_after":5,"baseline":"baseline","started_at":"`+startedAt.Format(time.RFC3339)+`"}}
}`)

	canary := &CanaryConfig{Provider: "canary", TrafficPct: 0.25, PromoteAfter: 5, Baseline: "baseline", StartedAt: startedAt.Format(time.RFC3339)}
	oc := &OpenAIClients{}
	snapshot := &routingSnapshot{routing: RoutingConfig{Canary: canary, EventsPath: eventsPath}, configs: map[string]Config{
		"baseline": {Priority: 7}, "canary": {Priority: 19},
	}}
	if err := oc.evaluateAndPublishCanary(context.Background(), snapshot); err != nil {
		t.Fatalf("promote: %v", err)
	}

	data, err := os.ReadFile(ConfigFile())
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	if strings.Contains(string(data), envOnlyKey) {
		t.Fatal("persisted config contains environment-only API key")
	}
	cfg, err := LoadRawGlobalConfig()
	if err != nil {
		t.Fatalf("LoadRawGlobalConfig: %v", err)
	}
	if cfg.Routing.Canary != nil {
		t.Fatalf("persisted canary = %#v, want removed", cfg.Routing.Canary)
	}
	if got := cfg.Providers["canary"].Priority; got != 7 {
		t.Fatalf("canary priority = %d, want baseline priority 7", got)
	}
	if got := cfg.Providers["canary"].APIKey; got != "" {
		t.Fatalf("canary API key = %q, want environment-only key omitted", got)
	}
	if got := cfg.Providers["baseline"].APIKey; got != "configured-baseline-key" {
		t.Fatalf("baseline API key = %q, want explicit configured key preserved", got)
	}
}

func TestExecuteChatAttemptWithKeyRotation_RetriesOnRateLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	calls := 0
	rotated := 0

	result, err := executeChatAttemptWithKeyRotation(
		ctx,
		"openai",
		Config{Model: "gpt-5-mini"},
		"sys",
		"user",
		func(provider string, result chatRequestResult) (bool, error) {
			if provider != "openai" {
				t.Fatalf("unexpected provider %q", provider)
			}
			if result.clientKey != "" {
				t.Fatalf("unexpected client key %q", result.clientKey)
			}
			rotated++
			return true, nil
		},
		func(context.Context, string, Config, string, string) (chatRequestResult, error) {
			calls++
			if calls == 1 {
				return chatRequestResult{}, errors.New("429 rate limit exceeded")
			}
			return chatRequestResult{content: "ok"}, nil
		},
	)

	if err != nil {
		t.Fatalf("expected retry success, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
	if rotated != 1 {
		t.Fatalf("expected 1 rotation, got %d", rotated)
	}
	if result.content != "ok" {
		t.Fatalf("expected successful content after retry, got %q", result.content)
	}
}

func TestFinalizeChatRequest(t *testing.T) {
	t.Parallel()
	t.Run("fills missing cost and usage", func(t *testing.T) {
		t.Parallel()
		usage := &LLMUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30}
		start := time.Now().Add(-25 * time.Millisecond)

		result, err := finalizeChatRequest("openai", Config{Model: "gpt-5-mini"}, start, "hello", usage, "stop", nil, nil)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if result.content != "hello" {
			t.Fatalf("expected content to be preserved, got %q", result.content)
		}
		if result.usage == nil || result.usage.Cost == nil {
			t.Fatalf("expected missing cost to be backfilled, got %+v", result.usage)
		}
		if result.duration <= 0 {
			t.Fatalf("expected positive duration, got %s", result.duration)
		}
	})

	t.Run("wraps backend errors and preserves duration", func(t *testing.T) {
		t.Parallel()
		start := time.Now().Add(-10 * time.Millisecond)
		_, err := finalizeChatRequest("groq", Config{Model: "llama"}, start, "", nil, "", nil, errors.New("boom"))
		if err == nil {
			t.Fatal("expected wrapped error")
		}
		if !strings.Contains(err.Error(), "groq: boom") {
			t.Fatalf("expected backend-wrapped error, got %v", err)
		}
	})
}
