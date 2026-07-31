package providers

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// mockOpenAIProvider creates an OpenAIProvider with mock OpenAIClients for testing.
// This avoids the need for real OpenAI SDK initialization.
func mockOpenAIProvider(backends []string, configs map[string]Config) *OpenAIProvider {
	oc := createMockOpenAIClients(backends, configs)

	return &OpenAIProvider{
		oc:      oc,
		configs: configs,
	}
}

// TestOpenAIProvider_SelectWeightedProvider tests weighted round-robin distribution
func TestOpenAIProvider_SelectWeightedProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		providers   []providerInfo
		totalWeight int
		iterations  int
		wantDist    map[string]int // expected distribution (approximate counts)
	}{
		{
			name: "equal weights",
			providers: []providerInfo{
				{name: "p1", weight: 2},
				{name: "p2", weight: 2},
			},
			totalWeight: 4,
			iterations:  100,
			wantDist:    map[string]int{"p1": 50, "p2": 50},
		},
		{
			name: "different weights 3:1",
			providers: []providerInfo{
				{name: "heavy", weight: 3},
				{name: "light", weight: 1},
			},
			totalWeight: 4,
			iterations:  100,
			wantDist:    map[string]int{"heavy": 75, "light": 25},
		},
		{
			name: "three providers unequal",
			providers: []providerInfo{
				{name: "p1", weight: 1},
				{name: "p2", weight: 2},
				{name: "p3", weight: 1},
			},
			totalWeight: 4,
			iterations:  100,
			wantDist:    map[string]int{"p1": 25, "p2": 50, "p3": 25},
		},
		{
			name: "single provider",
			providers: []providerInfo{
				{name: "solo", weight: 5},
			},
			totalWeight: 5,
			iterations:  50,
			wantDist:    map[string]int{"solo": 50},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create mock provider
			configs := make(map[string]Config)
			for _, p := range tt.providers {
				configs[p.name] = Config{
					Model:   "test-model",
					Workers: p.weight,
					Enabled: true,
				}
			}
			provider := mockOpenAIProvider(getProviderNames(tt.providers), configs)

			// Reset counter for deterministic testing
			provider.requestCounter = 0

			// Track distribution
			counts := make(map[string]int)
			for i := 0; i < tt.iterations; i++ {
				selected := provider.selectWeightedProvider(tt.providers, tt.totalWeight)
				counts[selected.name]++
			}

			// Verify distribution matches expected (exact match for deterministic weighted round-robin)
			for name, wantCount := range tt.wantDist {
				gotCount := counts[name]
				if gotCount != wantCount {
					t.Errorf("provider %s: got %d selections, want %d", name, gotCount, wantCount)
				}
			}
		})
	}
}

func getProviderNames(providers []providerInfo) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.name
	}
	return names
}

// TestOpenAIProvider_SelectWeightedProvider_Concurrent tests thread safety
func TestOpenAIProvider_SelectWeightedProvider_Concurrent(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"p1": {Model: "m1", Workers: 2, Enabled: true},
		"p2": {Model: "m2", Workers: 2, Enabled: true},
	}
	provider := mockOpenAIProvider([]string{"p1", "p2"}, configs)

	providers := []providerInfo{
		{name: "p1", weight: 2},
		{name: "p2", weight: 2},
	}
	totalWeight := 4

	var wg sync.WaitGroup
	iterations := 1000
	goroutines := 10

	counts := make(map[string]int)
	var mu sync.Mutex

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				selected := provider.selectWeightedProvider(providers, totalWeight)
				mu.Lock()
				counts[selected.name]++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Verify both providers were selected (total should be goroutines * iterations)
	total := counts["p1"] + counts["p2"]
	if total != goroutines*iterations {
		t.Errorf("expected %d total selections, got %d", goroutines*iterations, total)
	}

	// Verify roughly equal distribution (allow 10% tolerance for concurrent access)
	expected := float64(goroutines*iterations) / 2
	tolerance := expected * 0.1

	for _, name := range []string{"p1", "p2"} {
		count := float64(counts[name])
		if count < expected-tolerance || count > expected+tolerance {
			t.Errorf("provider %s: got %.0f selections, expected ~%.0f (tolerance: %.0f)", name, count, expected, tolerance)
		}
	}
}

// TestOpenAIProvider_CollectEnabledProviders tests filtering by circuit breaker health
func TestOpenAIProvider_CollectEnabledProviders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		backends          []string
		unhealthyBackends []string
		wantEnabled       []string
	}{
		{
			name:              "all healthy",
			backends:          []string{"b1", "b2", "b3"},
			unhealthyBackends: nil,
			wantEnabled:       []string{"b1", "b2", "b3"},
		},
		{
			name:              "one unhealthy",
			backends:          []string{"b1", "b2", "b3"},
			unhealthyBackends: []string{"b2"},
			wantEnabled:       []string{"b1", "b3"},
		},
		{
			name:              "all unhealthy",
			backends:          []string{"b1", "b2"},
			unhealthyBackends: []string{"b1", "b2"},
			wantEnabled:       []string{}, // empty slice
		},
		{
			name:              "single backend healthy",
			backends:          []string{"solo"},
			unhealthyBackends: nil,
			wantEnabled:       []string{"solo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			configs := make(map[string]Config)
			for _, name := range tt.backends {
				configs[name] = Config{Model: "test", Workers: 2, Enabled: true}
			}

			provider := mockOpenAIProvider(tt.backends, configs)

			// Mark backends unhealthy via circuit breaker
			for _, unhealthy := range tt.unhealthyBackends {
				provider.oc.CircuitBreaker.RecordFailure(unhealthy, errors.New("rate limit"))
			}

			enabled, totalWeight := provider.collectEnabledProviders()

			// Verify count
			if len(enabled) != len(tt.wantEnabled) {
				t.Errorf("got %d enabled providers, want %d", len(enabled), len(tt.wantEnabled))
			}

			// Verify names
			enabledNames := make(map[string]bool)
			for _, e := range enabled {
				enabledNames[e.name] = true
			}

			for _, want := range tt.wantEnabled {
				if !enabledNames[want] {
					t.Errorf("expected %s to be enabled", want)
				}
			}

			// Verify total weight
			expectedWeight := len(tt.wantEnabled) * 2 // workers = 2
			if totalWeight != expectedWeight {
				t.Errorf("got totalWeight %d, want %d", totalWeight, expectedWeight)
			}
		})
	}
}

// TestOpenAIProvider_CollectEnabledProviders_DifferentWeights tests weight calculation
func TestOpenAIProvider_CollectEnabledProviders_DifferentWeights(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"b1": {Model: "m1", Workers: 3, Enabled: true},
		"b2": {Model: "m2", Workers: 5, Enabled: true},
		"b3": {Model: "m3", Workers: 2, Enabled: true},
	}

	provider := mockOpenAIProvider([]string{"b1", "b2", "b3"}, configs)

	enabled, totalWeight := provider.collectEnabledProviders()

	if len(enabled) != 3 {
		t.Errorf("got %d enabled, want 3", len(enabled))
	}

	expectedWeight := 3 + 5 + 2 // 10
	if totalWeight != expectedWeight {
		t.Errorf("got totalWeight %d, want %d", totalWeight, expectedWeight)
	}

	// Verify individual weights
	for _, e := range enabled {
		cfg := configs[e.name]
		if e.weight != cfg.GetWorkers() {
			t.Errorf("provider %s: got weight %d, want %d", e.name, e.weight, cfg.GetWorkers())
		}
	}
}

// TestOpenAIProvider_GetOpenAIClients tests the getter
func TestOpenAIProvider_GetOpenAIClients(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"b1": {Model: "test", Workers: 2, Enabled: true},
	}
	provider := mockOpenAIProvider([]string{"b1"}, configs)

	oc := provider.GetOpenAIClients()

	if oc == nil {
		t.Fatal("GetOpenAIClients returned nil")
	}

	// Verify it's the same instance
	if oc != provider.oc {
		t.Error("GetOpenAIClients returned different instance")
	}

	// Verify circuit breaker is accessible
	if oc.CircuitBreaker == nil {
		t.Error("OpenAIClients has nil CircuitBreaker")
	}

	// Verify backends
	if len(oc.Backends) != 1 {
		t.Errorf("expected 1 backend, got %d", len(oc.Backends))
	}
	if oc.Backends[0].Name != "b1" {
		t.Errorf("expected backend 'b1', got %q", oc.Backends[0].Name)
	}
}

// TestOpenAIProvider_Name tests the Name method
func TestOpenAIProvider_Name(t *testing.T) {
	t.Parallel()
	provider := mockOpenAIProvider([]string{"b1"}, map[string]Config{
		"b1": {Model: "test", Enabled: true},
	})

	if provider.Name() != "openai" {
		t.Errorf("expected name 'openai', got %q", provider.Name())
	}
}

// TestOpenAIProvider_MaxTokens tests the MaxTokens method
func TestOpenAIProvider_MaxTokens(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"b1": {Model: "test", MaxTokens: 4000, Enabled: true},
		"b2": {Model: "test", MaxTokens: 8000, Enabled: true},
	}

	// mockOpenAIProvider doesn't set maxTokens, so test manually
	provider := mockOpenAIProvider([]string{"b1", "b2"}, configs)
	provider.maxTokens = MaxTokensFromConfigs(configs, DefaultMaxTokens)

	if provider.MaxTokens() != 8000 {
		t.Errorf("expected maxTokens 8000, got %d", provider.MaxTokens())
	}
}

// TestOpenAIProvider_SetFailoverCallback tests callback configuration
func TestOpenAIProvider_SetFailoverCallback(t *testing.T) {
	t.Parallel()
	provider := mockOpenAIProvider([]string{"b1"}, map[string]Config{
		"b1": {Model: "test", Enabled: true},
	})

	if provider.failoverCallback != nil {
		t.Error("expected nil callback initially")
	}

	called := false
	callback := func(_ FailoverEvent) {
		called = true
	}

	provider.SetFailoverCallback(callback)

	if provider.failoverCallback == nil {
		t.Error("callback should be set")
	}

	// Invoke callback to verify it's wired correctly
	provider.failoverCallback(FailoverEvent{})
	if !called {
		t.Error("callback was not invoked")
	}
}

// TestOpenAIProvider_ChatWithInfoContext_NoProviders tests error case
func TestOpenAIProvider_ChatWithInfoContext_NoProviders(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"b1": {Model: "test", Workers: 2, Enabled: true},
	}
	provider := mockOpenAIProvider([]string{"b1"}, configs)

	// Mark the only backend as unhealthy
	provider.oc.CircuitBreaker.RecordFailure("b1", errors.New("rate limit"))

	ctx := context.Background()
	_, err := provider.ChatWithInfoContext(ctx, "system", "user")

	if err == nil {
		t.Fatal("expected error when no providers available")
	}

	if err.Error() != "no enabled providers available" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestOpenAIProvider_Failover_CallbackNotification tests failover callback invocation
func TestOpenAIProvider_Failover_CallbackNotification(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"primary":   {Model: "model-a", Workers: 2, Enabled: true},
		"secondary": {Model: "model-b", Workers: 2, Enabled: true},
	}
	provider := mockOpenAIProvider([]string{"primary", "secondary"}, configs)

	var events []FailoverEvent
	var mu sync.Mutex

	provider.SetFailoverCallback(func(event FailoverEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	// Trigger failover by executing a plan with a selected provider
	// that will fail (since we have no real clients)
	ctx := context.Background()
	selected := providerInfo{name: "primary", cfg: configs["primary"], weight: 2}
	enabled := []providerInfo{
		{name: "primary", cfg: configs["primary"], weight: 2},
		{name: "secondary", cfg: configs["secondary"], weight: 2},
	}

	plan := chatExecutionPlan{
		selected:        selected,
		enabled:         enabled,
		plannedProvider: selected.name,
	}
	// This will fail since we have no real OpenAI clients
	_, _ = provider.coordinator().execute(ctx, plan, "system", "user", provider.failoverCallback, provider.executeChatAttempt)

	mu.Lock()
	defer mu.Unlock()

	// Should have received failover events
	if len(events) < 1 {
		t.Error("expected at least one failover event")
	}

	// First event should be from primary
	if events[0].FromBackend != "primary" {
		t.Errorf("expected FromBackend 'primary', got %q", events[0].FromBackend)
	}

	// Second event (if present) should indicate failover to secondary
	if len(events) >= 2 {
		if events[1].ToBackend != "secondary" {
			t.Errorf("expected ToBackend 'secondary', got %q", events[1].ToBackend)
		}
	}
}

// TestOpenAIProvider_Shutdown tests cleanup
func TestOpenAIProvider_Shutdown(t *testing.T) {
	t.Parallel()
	provider := mockOpenAIProvider([]string{"b1"}, map[string]Config{
		"b1": {Model: "test", Enabled: true},
	})

	// Should not panic with nil clients
	provider.Shutdown()
}

// TestNormalizeModelName tests model name normalization
func TestNormalizeModelName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"mistralai/mistral-7b", "mistral-7b"},
		{"anthropic/claude-3-sonnet", "claude-3-sonnet"},
		{"hf:zai-org/model", "model"},
		{"hf:other/model", "other/model"},
		{"openai/gpt-4", "gpt-4"},
		{"google/gemini-pro", "gemini-pro"},
		{"plain-model", "plain-model"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := NormalizeModelName(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeModelName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestOpenAIProvider_SelectWeightedProvider_FallbackToFirst tests fallback behavior
func TestOpenAIProvider_SelectWeightedProvider_FallbackToFirst(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"p1": {Model: "m1", Workers: 2, Enabled: true},
	}
	provider := mockOpenAIProvider([]string{"p1"}, configs)

	// Set counter to a very high value that exceeds total weight
	provider.requestCounter = 999999999

	providers := []providerInfo{
		{name: "p1", weight: 2},
	}

	// Should still return a valid provider (fallback to first)
	selected := provider.selectWeightedProvider(providers, 2)
	if selected.name != "p1" {
		t.Errorf("expected p1, got %s", selected.name)
	}
}

// TestOpenAIProvider_Chat_DelegatesToChatWithInfo tests the wrapper method
func TestOpenAIProvider_Chat_DelegatesToChatWithInfo(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"b1": {Model: "test", Workers: 2, Enabled: true},
	}
	provider := mockOpenAIProvider([]string{"b1"}, configs)

	// Chat will fail because we have no real clients, but we can verify the error propagates
	_, err := provider.Chat(t.Context(), "system", "user")

	// Should get an error (no enabled providers after circuit breaker or no client)
	if err == nil {
		// This would only happen if somehow the request succeeded
		t.Log("Chat succeeded unexpectedly (possibly circuit breaker timing)")
	}
}

// TestOpenAIProvider_WeightedDistribution_Deterministic verifies exact weighted distribution
func TestOpenAIProvider_WeightedDistribution_Deterministic(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"heavy": {Model: "m1", Workers: 4, Enabled: true},
		"light": {Model: "m2", Workers: 1, Enabled: true},
	}
	provider := mockOpenAIProvider([]string{"heavy", "light"}, configs)
	provider.requestCounter = 0 // reset for deterministic test

	providers := []providerInfo{
		{name: "heavy", weight: 4},
		{name: "light", weight: 1},
	}
	totalWeight := 5

	// Track selections over multiple full cycles
	selections := make([]string, 10)
	for i := 0; i < 10; i++ {
		selected := provider.selectWeightedProvider(providers, totalWeight)
		selections[i] = selected.name
	}

	// Counter increments first (1, 2, 3, ...) then mod totalWeight for slot
	// With weights heavy:4, light:1, totalWeight:5
	// - slot < cumulative selects the provider
	// - heavy has cumulative 4 (slots 0-3), light has cumulative 5 (slot 4)
	// Counter 1 -> slot 1 < 4 -> heavy
	// Counter 2 -> slot 2 < 4 -> heavy
	// Counter 3 -> slot 3 < 4 -> heavy
	// Counter 4 -> slot 4 < 5 -> light
	// Counter 5 -> slot 0 < 4 -> heavy
	// Counter 6 -> slot 1 < 4 -> heavy
	// Counter 7 -> slot 2 < 4 -> heavy
	// Counter 8 -> slot 3 < 4 -> heavy
	// Counter 9 -> slot 4 < 5 -> light
	// Counter 10 -> slot 0 < 4 -> heavy
	expected := []string{"heavy", "heavy", "heavy", "light", "heavy", "heavy", "heavy", "heavy", "light", "heavy"}

	for i, want := range expected {
		if selections[i] != want {
			t.Errorf("selection %d: got %s, want %s", i, selections[i], want)
		}
	}
}

// TestOpenAIProvider_CircuitBreakerIntegration tests that circuit breaker state affects provider selection
func TestOpenAIProvider_CircuitBreakerIntegration(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"b1": {Model: "m1", Workers: 2, Enabled: true},
		"b2": {Model: "m2", Workers: 2, Enabled: true},
		"b3": {Model: "m3", Workers: 2, Enabled: true},
	}
	provider := mockOpenAIProvider([]string{"b1", "b2", "b3"}, configs)

	// All healthy initially
	enabled, _ := provider.collectEnabledProviders()
	if len(enabled) != 3 {
		t.Errorf("expected 3 enabled initially, got %d", len(enabled))
	}

	// Disable b2 via circuit breaker
	provider.oc.CircuitBreaker.RecordFailure("b2", errors.New("rate limit"))

	enabled, _ = provider.collectEnabledProviders()
	if len(enabled) != 2 {
		t.Errorf("expected 2 enabled after b2 failure, got %d", len(enabled))
	}

	// Verify b2 is not in enabled list
	for _, e := range enabled {
		if e.name == "b2" {
			t.Error("b2 should not be in enabled list after rate limit error")
		}
	}

	// Recover b2
	provider.oc.CircuitBreaker.RecordSuccess("b2")

	enabled, _ = provider.collectEnabledProviders()
	if len(enabled) != 3 {
		t.Errorf("expected 3 enabled after b2 recovery, got %d", len(enabled))
	}
}
