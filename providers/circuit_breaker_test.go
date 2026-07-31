package providers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	t.Parallel()
	backends := []string{"openai", "anthropic", "openrouter"}
	cb := NewCircuitBreaker(backends, nil)

	for _, name := range backends {
		if !cb.IsHealthy(name) {
			t.Errorf("backend %q should be healthy initially", name)
		}

		health := cb.GetHealth(name)
		if health.Failures != 0 {
			t.Errorf("backend %q should have 0 failures initially, got %d", name, health.Failures)
		}
		if health.Disabled {
			t.Errorf("backend %q should not be disabled initially", name)
		}
	}
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker([]string{"test-backend"}, nil)
	err := errors.New("connection refused")

	cb.RecordFailure("test-backend", err)

	health := cb.GetHealth("test-backend")
	if health.Failures != 1 {
		t.Errorf("expected 1 failure, got %d", health.Failures)
	}
	if health.LastFailure.IsZero() {
		t.Error("LastFailure should be set")
	}
	if health.Disabled {
		t.Error("should not be disabled after 1 failure")
	}
}

func TestCircuitBreaker_DisableAfterConsecutiveFailures(t *testing.T) {
	t.Parallel()
	var callbackCalled bool
	var callbackEvent CircuitBreakerEvent

	cb := NewCircuitBreaker([]string{"test-backend"}, func(event CircuitBreakerEvent) {
		callbackCalled = true
		callbackEvent = event
	})

	err := errors.New("connection refused")

	// Record failures up to but not reaching threshold
	for i := 0; i < MaxConsecutiveFailures-1; i++ {
		cb.RecordFailure("test-backend", err)
		if cb.GetHealth("test-backend").Disabled {
			t.Errorf("should not be disabled after %d failures", i+1)
		}
	}

	// This failure should trigger disable
	cb.RecordFailure("test-backend", err)

	health := cb.GetHealth("test-backend")
	if !health.Disabled {
		t.Errorf("should be disabled after %d failures", MaxConsecutiveFailures)
	}
	if health.Failures != MaxConsecutiveFailures {
		t.Errorf("expected %d failures, got %d", MaxConsecutiveFailures, health.Failures)
	}

	// Verify callback was called
	if !callbackCalled {
		t.Error("callback should have been called on disable")
	}
	if callbackEvent.EventType != "disabled" {
		t.Errorf("expected event type 'disabled', got %q", callbackEvent.EventType)
	}
	if callbackEvent.Backend != "test-backend" {
		t.Errorf("expected backend 'test-backend', got %q", callbackEvent.Backend)
	}
	if callbackEvent.Failures != MaxConsecutiveFailures {
		t.Errorf("expected %d failures in event, got %d", MaxConsecutiveFailures, callbackEvent.Failures)
	}
	if callbackEvent.IsQuota {
		t.Error("should not be marked as quota error")
	}
}

func TestCircuitBreaker_RateLimitImmediateDisable(t *testing.T) {
	t.Parallel()
	var callbackEvent CircuitBreakerEvent

	cb := NewCircuitBreaker([]string{"test-backend"}, func(event CircuitBreakerEvent) {
		callbackEvent = event
	})

	// Rate limit error should trigger immediate disable (first failure)
	err := errors.New("rate limit exceeded")
	cb.RecordFailure("test-backend", err)

	health := cb.GetHealth("test-backend")
	if !health.Disabled {
		t.Error("should be immediately disabled on rate limit error")
	}
	if health.Failures != 1 {
		t.Errorf("expected 1 failure, got %d", health.Failures)
	}
	if callbackEvent.EventType != "disabled" {
		t.Errorf("expected event type 'disabled', got %q", callbackEvent.EventType)
	}
	if !callbackEvent.IsQuota {
		t.Error("should be marked as quota error for rate limit")
	}
}

func TestCircuitBreaker_QuotaExceededImmediateDisable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		errMsg string
	}{
		{"quota exceeded", "quota exceeded"},
		{"quota_exceeded", "quota_exceeded"},
		{"HTTP 429", "status code: 429"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var callbackEvent CircuitBreakerEvent

			cb := NewCircuitBreaker([]string{"test-backend"}, func(event CircuitBreakerEvent) {
				callbackEvent = event
			})

			err := errors.New(tt.errMsg)
			cb.RecordFailure("test-backend", err)

			health := cb.GetHealth("test-backend")
			if !health.Disabled {
				t.Errorf("should be immediately disabled on error: %s", tt.errMsg)
			}
			if !callbackEvent.IsQuota {
				t.Error("should be marked as quota error")
			}
		})
	}
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	t.Parallel()
	var recoveredCalled bool

	cb := NewCircuitBreaker([]string{"test-backend"}, func(event CircuitBreakerEvent) {
		if event.EventType == "recovered" {
			recoveredCalled = true
		}
	})

	// First disable the backend
	err := errors.New("rate limit exceeded")
	cb.RecordFailure("test-backend", err)

	if !cb.GetHealth("test-backend").Disabled {
		t.Error("should be disabled")
	}

	// Record success to recover
	cb.RecordSuccess("test-backend")

	health := cb.GetHealth("test-backend")
	if health.Disabled {
		t.Error("should not be disabled after success")
	}
	if health.Failures != 0 {
		t.Errorf("failures should be reset to 0, got %d", health.Failures)
	}
	if !recoveredCalled {
		t.Error("recovered callback should have been called")
	}
}

func TestCircuitBreaker_RecordSuccess_NoCallbackWhenNotDisabled(t *testing.T) {
	t.Parallel()
	callbackCalled := false

	cb := NewCircuitBreaker([]string{"test-backend"}, func(_ CircuitBreakerEvent) {
		callbackCalled = true
	})

	// Record some failures (but not enough to disable)
	cb.RecordFailure("test-backend", errors.New("transient error"))

	// Record success
	cb.RecordSuccess("test-backend")

	if callbackCalled {
		t.Error("callback should not be called when backend was not disabled")
	}

	health := cb.GetHealth("test-backend")
	if health.Failures != 0 {
		t.Errorf("failures should be reset to 0, got %d", health.Failures)
	}
}

func TestCircuitBreaker_CooldownExpiry(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker([]string{"test-backend"}, nil)

	// Disable via consecutive failures (not rate limit)
	err := errors.New("connection refused")
	for i := 0; i < MaxConsecutiveFailures; i++ {
		cb.RecordFailure("test-backend", err)
	}

	// Verify disabled
	if cb.IsHealthy("test-backend") {
		// If healthy, the cooldown check passed (time.Since > FailureCooldown)
		// This means the test is running slowly, skip verifying disabled state
		t.Skip("cooldown already expired, skipping time-sensitive test")
	}

	// Manually set DisabledAt to past to simulate cooldown expiry
	cb.mu.Lock()
	h := cb.health["test-backend"]
	h.DisabledAt = time.Now().Add(-FailureCooldown - time.Second)
	cb.mu.Unlock()

	// Should be healthy again after cooldown
	if !cb.IsHealthy("test-backend") {
		t.Error("should be healthy after failure cooldown expires")
	}
}

func TestCircuitBreaker_RateLimitCooldownExpiry(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker([]string{"test-backend"}, nil)

	// Disable via rate limit (sets higher failure count to trigger rate limit cooldown)
	// First, accumulate enough failures to exceed QuotaErrorThreshold
	for i := 0; i < QuotaErrorThreshold; i++ {
		cb.RecordFailure("test-backend", errors.New("rate limit"))
	}

	// Verify disabled
	health := cb.GetHealth("test-backend")
	if !health.Disabled {
		t.Error("should be disabled")
	}
	if health.Failures < QuotaErrorThreshold {
		t.Errorf("failures should be >= %d, got %d", QuotaErrorThreshold, health.Failures)
	}

	// With failure count >= QuotaErrorThreshold, should use RateLimitCooldown
	// Set DisabledAt to just past FailureCooldown but before RateLimitCooldown
	cb.mu.Lock()
	h := cb.health["test-backend"]
	h.DisabledAt = time.Now().Add(-FailureCooldown - time.Second)
	cb.mu.Unlock()

	// Should still be unhealthy (rate limit cooldown is longer)
	if cb.IsHealthy("test-backend") {
		t.Error("should not be healthy - rate limit cooldown (5m) should apply, not failure cooldown (60s)")
	}

	// Now set to past rate limit cooldown
	cb.mu.Lock()
	h = cb.health["test-backend"]
	h.DisabledAt = time.Now().Add(-RateLimitCooldown - time.Second)
	cb.mu.Unlock()

	// Now should be healthy
	if !cb.IsHealthy("test-backend") {
		t.Error("should be healthy after rate limit cooldown expires")
	}
}

func TestCircuitBreaker_Callback(t *testing.T) {
	t.Parallel()
	events := make([]CircuitBreakerEvent, 0)
	var mu sync.Mutex

	cb := NewCircuitBreaker([]string{"backend1", "backend2"}, func(event CircuitBreakerEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	// Disable backend1 via rate limit
	cb.RecordFailure("backend1", errors.New("rate limit"))

	// Disable backend2 via consecutive failures
	for i := 0; i < MaxConsecutiveFailures; i++ {
		cb.RecordFailure("backend2", errors.New("timeout"))
	}

	// Recover backend1
	cb.RecordSuccess("backend1")

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Event 1: backend1 disabled (rate limit)
	if events[0].Backend != "backend1" || events[0].EventType != "disabled" || !events[0].IsQuota {
		t.Errorf("event[0] unexpected: %+v", events[0])
	}

	// Event 2: backend2 disabled (consecutive failures)
	if events[1].Backend != "backend2" || events[1].EventType != "disabled" || events[1].IsQuota {
		t.Errorf("event[1] unexpected: %+v", events[1])
	}

	// Event 3: backend1 recovered
	if events[2].Backend != "backend1" || events[2].EventType != "recovered" {
		t.Errorf("event[2] unexpected: %+v", events[2])
	}
}

func TestCircuitBreaker_Concurrent(t *testing.T) {
	t.Parallel()
	backends := []string{"backend1", "backend2", "backend3"}
	var eventCount atomic.Int64

	cb := NewCircuitBreaker(backends, func(_ CircuitBreakerEvent) {
		eventCount.Add(1)
	})

	var wg sync.WaitGroup
	iterations := 100

	// Spawn goroutines that concurrently access the circuit breaker
	for _, backend := range backends {
		// Reader goroutine
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = cb.IsHealthy(name)
				_ = cb.GetHealth(name)
			}
		}(backend)

		// Failure recorder goroutine
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				cb.RecordFailure(name, errors.New("test error"))
			}
		}(backend)

		// Success recorder goroutine
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				cb.RecordSuccess(name)
			}
		}(backend)
	}

	wg.Wait()

	// Verify no panic occurred and data is consistent
	allHealth := cb.GetAllHealth()
	if len(allHealth) != len(backends) {
		t.Errorf("expected %d backends in health map, got %d", len(backends), len(allHealth))
	}

	// Some events should have fired
	if eventCount.Load() == 0 {
		t.Error("expected at least some events to fire during concurrent access")
	}
}

func TestCircuitBreaker_UnknownBackend(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker([]string{"known"}, nil)

	// Unknown backends should be considered healthy (nil check)
	if !cb.IsHealthy("unknown") {
		t.Error("unknown backend should be considered healthy")
	}

	// Recording on unknown backend should not panic
	cb.RecordFailure("unknown", errors.New("test"))
	cb.RecordSuccess("unknown")

	// GetHealth for unknown should return zero value
	health := cb.GetHealth("unknown")
	if health.Failures != 0 || health.Disabled {
		t.Error("unknown backend health should be zero value")
	}
}

func TestCircuitBreaker_GetAllHealth(t *testing.T) {
	t.Parallel()
	backends := []string{"backend1", "backend2"}
	cb := NewCircuitBreaker(backends, nil)

	// Add some failures to backend1
	cb.RecordFailure("backend1", errors.New("error"))
	cb.RecordFailure("backend1", errors.New("error"))

	allHealth := cb.GetAllHealth()

	if len(allHealth) != 2 {
		t.Fatalf("expected 2 backends, got %d", len(allHealth))
	}

	if allHealth["backend1"].Failures != 2 {
		t.Errorf("expected 2 failures for backend1, got %d", allHealth["backend1"].Failures)
	}

	if allHealth["backend2"].Failures != 0 {
		t.Errorf("expected 0 failures for backend2, got %d", allHealth["backend2"].Failures)
	}
}

func TestCircuitBreaker_NilCallback(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker([]string{"test"}, nil)

	// Should not panic when callback is nil
	cb.RecordFailure("test", errors.New("rate limit"))
	cb.RecordSuccess("test")

	// Just verify no panic occurred
	if cb.GetHealth("test").Failures != 0 {
		t.Error("failures should be reset after success")
	}
}

func TestCircuitBreaker_CallbackOnlyOncePerDisable(t *testing.T) {
	t.Parallel()
	var disableCount int

	cb := NewCircuitBreaker([]string{"test"}, func(event CircuitBreakerEvent) {
		if event.EventType == "disabled" {
			disableCount++
		}
	})

	// Record many failures after already disabled
	for i := 0; i < 10; i++ {
		cb.RecordFailure("test", errors.New("rate limit"))
	}

	// Callback should only fire once (on first disable)
	if disableCount != 1 {
		t.Errorf("expected 1 disable callback, got %d", disableCount)
	}
}

func TestKeyClientInitFailureDoesNotTripBreaker(t *testing.T) {
	t.Parallel()

	const backend = "openai"
	cb := NewCircuitBreaker([]string{backend}, nil)

	// Simulate the chat path: request returns a 429, rotation selects a new
	// key but client construction fails (ErrKeyClientInit). The helper must
	// surface the distinct error, and RecordFailure must leave the breaker
	// healthy.
	rateLimitErr := NewOpenAIErrorFromMessage("429 Too Many Requests")
	require.True(t, IsRateLimitError(rateLimitErr))

	rotateKey := func(string, chatRequestResult) (bool, error) {
		return false, fmt.Errorf("%w: openai: dial fail", ErrKeyClientInit)
	}
	request := func(ctx context.Context, _ string, _ Config, _ string, _ string) (chatRequestResult, error) {
		return chatRequestResult{}, rateLimitErr
	}

	_, err := executeChatAttemptWithKeyRotation(context.Background(), backend, Config{}, "sys", "user", rotateKey, request)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrKeyClientInit)

	// The circuit breaker, fed this error, must NOT disable the backend.
	cb.RecordFailure(backend, err)
	require.True(t, cb.IsHealthy(backend), "client-init failure must not trip the breaker")
	require.Equal(t, 0, cb.GetHealth(backend).Failures)
	require.False(t, cb.GetHealth(backend).Disabled)
}

// Regression guard: a genuine 429 (no rotation possible) still trips the breaker.
func TestRealRateLimitStillTripsBreaker(t *testing.T) {
	t.Parallel()
	const backend = "openai"
	cb := NewCircuitBreaker([]string{backend}, nil)
	cb.RecordFailure(backend, NewOpenAIErrorFromMessage("429 Too Many Requests"))
	require.False(t, cb.IsHealthy(backend), "quota error must still disable the backend")
}

func TestCircuitBreaker_UnsupportedModelQuarantinesBackend(t *testing.T) {
	t.Parallel()
	const badModelBackend = "openai:gpt-bad"
	const goodModelBackend = "openai:gpt-good"
	cb := NewCircuitBreaker([]string{badModelBackend, goodModelBackend}, nil)

	cb.RecordFailure(badModelBackend, errors.New("invalid model specified"))

	badHealth := cb.GetHealth(badModelBackend)
	require.True(t, badHealth.Disabled)
	require.Equal(t, 1, badHealth.Failures)
	require.WithinDuration(t, badHealth.DisabledAt.Add(RateLimitCooldown), badHealth.CooldownUntil, time.Second)
	require.False(t, cb.IsHealthy(badModelBackend))
	require.True(t, cb.IsHealthy(goodModelBackend), "a model-specific failure must not quarantine sibling model backends")
}

func TestCircuitBreaker_ExplicitRetryAfterControlsRecovery(t *testing.T) {
	t.Parallel()
	const backend = "openai"
	cb := NewCircuitBreaker([]string{backend}, nil)

	cb.RecordFailure(backend, NewOpenAIErrorWithRetryAfter("429 Too Many Requests", 429, 2*time.Minute, nil))

	health := cb.GetHealth(backend)
	require.True(t, health.Disabled)
	require.WithinDuration(t, health.DisabledAt.Add(2*time.Minute), health.CooldownUntil, time.Second)

	cb.mu.Lock()
	cb.health[backend].DisabledAt = time.Now().Add(-FailureCooldown - time.Second)
	cb.health[backend].CooldownUntil = time.Now().Add(time.Minute)
	cb.mu.Unlock()
	require.False(t, cb.IsHealthy(backend), "explicit provider cooldown should override fixed failure cooldown")

	cb.mu.Lock()
	cb.health[backend].CooldownUntil = time.Now().Add(-time.Second)
	cb.mu.Unlock()
	require.True(t, cb.IsHealthy(backend), "backend should recover after explicit provider cooldown")
	require.True(t, cb.GetHealth(backend).CooldownUntil.IsZero(), "recovery should clear explicit cooldown")
}

func TestCircuitBreaker_RecordSuccessRespectsExplicitRetryAfter(t *testing.T) {
	t.Parallel()
	const backend = "openai"
	cb := NewCircuitBreaker([]string{backend}, nil)

	cb.RecordFailure(backend, NewOpenAIErrorWithRetryAfter("429 Too Many Requests", 429, 2*time.Minute, nil))
	cb.RecordSuccess(backend)
	require.True(t, cb.GetHealth(backend).Disabled, "success during explicit cooldown must not re-enable")

	cb.mu.Lock()
	cb.health[backend].CooldownUntil = time.Now().Add(-time.Second)
	cb.mu.Unlock()

	cb.RecordSuccess(backend)
	require.False(t, cb.GetHealth(backend).Disabled, "success after explicit cooldown should recover")
	require.True(t, cb.GetHealth(backend).CooldownUntil.IsZero())
}

func TestWrappedProviderErrorUsesWormholeRetryMetadata(t *testing.T) {
	t.Parallel()

	source := whtypes.NewWormholeError(whtypes.ErrorCodeRateLimit, "slow down", true).
		WithStatusCode(429).
		WithRetryAfter(7 * time.Second)
	wrapped := wrapProviderError(source, Config{Model: "test-model"})
	require.Equal(t, 7*time.Second, wrapped.RetryAfter())
}

// RecordSuccess must not clear a rate-limit quarantine while the cooldown is
// still active; once the cooldown expires, a success recovers the backend.
func TestCircuitBreaker_RecordSuccessRespectsRateLimitCooldown(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker([]string{"test-backend"}, nil)

	// Disable via enough rate-limit failures to exceed QuotaErrorThreshold,
	// which makes IsHealthy/RecordSuccess select RateLimitCooldown (5m).
	for i := 0; i < QuotaErrorThreshold; i++ {
		cb.RecordFailure("test-backend", errors.New("rate limit"))
	}

	health := cb.GetHealth("test-backend")
	if !health.Disabled {
		t.Fatal("should be disabled after quota-level rate-limit failures")
	}
	if health.Failures < QuotaErrorThreshold {
		t.Fatalf("failures should be >= %d, got %d", QuotaErrorThreshold, health.Failures)
	}

	// A success DURING the cooldown window must NOT re-enable the backend.
	cb.RecordSuccess("test-backend")
	health = cb.GetHealth("test-backend")
	if !health.Disabled {
		t.Error("RecordSuccess during rate-limit cooldown must not re-enable the backend")
	}
	if health.Failures < QuotaErrorThreshold {
		t.Errorf("failure count must be preserved during cooldown, got %d", health.Failures)
	}

	// Advance past the rate-limit cooldown, then a success recovers normally.
	cb.mu.Lock()
	cb.health["test-backend"].DisabledAt = time.Now().Add(-RateLimitCooldown - time.Second)
	cb.mu.Unlock()

	cb.RecordSuccess("test-backend")
	health = cb.GetHealth("test-backend")
	if health.Disabled {
		t.Error("RecordSuccess after rate-limit cooldown should re-enable the backend")
	}
	if health.Failures != 0 {
		t.Errorf("failures should be reset to 0 after recovery, got %d", health.Failures)
	}
}
