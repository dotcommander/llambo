package providers

import (
	"testing"
)

func TestCostTracker_NewCostTracker(t *testing.T) {
	t.Parallel()
	backends := []string{"openai", "anthropic", "openrouter"}
	ct := NewCostTracker(backends)

	if ct == nil {
		t.Fatal("expected non-nil CostTracker")
	}

	stats := ct.GetStats()
	if len(stats) != 3 {
		t.Errorf("expected 3 backends, got %d", len(stats))
	}

	for _, name := range backends {
		if _, ok := stats[name]; !ok {
			t.Errorf("expected backend %s to be tracked", name)
		}
	}
}

func TestCostTracker_Record(t *testing.T) {
	t.Parallel()
	ct := NewCostTracker([]string{"openai"})

	// Record usage with cost
	usage := &LLMUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		Cost: &LLMCost{
			TotalCost: 0.0015,
		},
	}

	ct.Record("openai", usage)

	stats := ct.GetStats()
	openaiStats := stats["openai"]

	if openaiStats.Requests != 1 {
		t.Errorf("expected 1 request, got %d", openaiStats.Requests)
	}
	if openaiStats.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", openaiStats.PromptTokens)
	}
	if openaiStats.CompletionTokens != 50 {
		t.Errorf("expected 50 completion tokens, got %d", openaiStats.CompletionTokens)
	}
	if openaiStats.TotalTokens != 150 {
		t.Errorf("expected 150 total tokens, got %d", openaiStats.TotalTokens)
	}
	if openaiStats.TotalCostUSD != 0.0015 {
		t.Errorf("expected cost 0.0015, got %f", openaiStats.TotalCostUSD)
	}
}

func TestCostTracker_RecordNilUsage(t *testing.T) {
	t.Parallel()
	ct := NewCostTracker([]string{"openai"})

	// Recording nil usage should not panic or change stats
	ct.Record("openai", nil)

	stats := ct.GetStats()
	if stats["openai"].Requests != 0 {
		t.Error("nil usage should not increment requests")
	}
}

func TestCostTracker_RecordUnknownBackend(t *testing.T) {
	t.Parallel()
	ct := NewCostTracker([]string{"openai"})

	usage := &LLMUsage{
		TotalTokens: 100,
	}

	// Recording to unknown backend should create entry
	ct.Record("unknown", usage)

	stats := ct.GetStats()
	if _, ok := stats["unknown"]; !ok {
		t.Error("expected unknown backend to be added")
	}
	if stats["unknown"].Requests != 1 {
		t.Errorf("expected 1 request for unknown backend, got %d", stats["unknown"].Requests)
	}
}

func TestCostTracker_GetTotal(t *testing.T) {
	t.Parallel()
	ct := NewCostTracker([]string{"openai", "anthropic"})

	ct.Record("openai", &LLMUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		Cost:             &LLMCost{TotalCost: 0.001},
	})

	ct.Record("anthropic", &LLMUsage{
		PromptTokens:     200,
		CompletionTokens: 100,
		TotalTokens:      300,
		Cost:             &LLMCost{TotalCost: 0.002},
	})

	total := ct.GetTotal()

	if total.Requests != 2 {
		t.Errorf("expected 2 total requests, got %d", total.Requests)
	}
	if total.PromptTokens != 300 {
		t.Errorf("expected 300 total prompt tokens, got %d", total.PromptTokens)
	}
	if total.CompletionTokens != 150 {
		t.Errorf("expected 150 total completion tokens, got %d", total.CompletionTokens)
	}
	if total.TotalTokens != 450 {
		t.Errorf("expected 450 total tokens, got %d", total.TotalTokens)
	}
	if total.TotalCostUSD != 0.003 {
		t.Errorf("expected total cost 0.003, got %f", total.TotalCostUSD)
	}
}

func TestCostTracker_Reset(t *testing.T) {
	t.Parallel()
	ct := NewCostTracker([]string{"openai"})

	ct.Record("openai", &LLMUsage{
		TotalTokens: 100,
		Cost:        &LLMCost{TotalCost: 0.001},
	})

	ct.Reset()

	stats := ct.GetStats()
	if stats["openai"].Requests != 0 {
		t.Error("expected stats to be reset")
	}
	if stats["openai"].TotalTokens != 0 {
		t.Error("expected tokens to be reset")
	}
}

func TestCostTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	ct := NewCostTracker([]string{"openai", "anthropic"})

	// Simulate concurrent access
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			ct.Record("openai", &LLMUsage{TotalTokens: 10})
			ct.GetStats()
			ct.GetTotal()
			done <- true
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	total := ct.GetTotal()
	if total.Requests != 100 {
		t.Errorf("expected 100 requests after concurrent access, got %d", total.Requests)
	}
}

func TestCostTracker_UsageWithoutCost(t *testing.T) {
	t.Parallel()
	ct := NewCostTracker([]string{"openai"})

	// Some providers may not return cost info
	usage := &LLMUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		Cost:             nil, // no cost info
	}

	ct.Record("openai", usage)

	stats := ct.GetStats()
	if stats["openai"].TotalCostUSD != 0 {
		t.Errorf("expected 0 cost when Cost is nil, got %f", stats["openai"].TotalCostUSD)
	}
	if stats["openai"].TotalTokens != 150 {
		t.Errorf("tokens should still be recorded, got %d", stats["openai"].TotalTokens)
	}
}
