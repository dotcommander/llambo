package providers

import (
	"context"
	"errors"
	"sync"
	"time"
)

// CircuitBreakerEvent represents a circuit breaker state change
type CircuitBreakerEvent struct {
	Backend   string
	EventType string // "disabled", "recovered"
	Failures  int
	IsQuota   bool
}

// CircuitBreakerCallback is called when circuit breaker state changes
type CircuitBreakerCallback func(event CircuitBreakerEvent)

// HealthRecorder defines the interface for recording backend health events.
// CircuitBreaker implements this interface.
type HealthRecorder interface {
	IsHealthy(name string) bool
	RecordSuccess(name string)
	RecordFailure(name string, err error)
}

// Ensure CircuitBreaker implements HealthRecorder
var _ HealthRecorder = (*CircuitBreaker)(nil)

// BackendHealth tracks health state for circuit breaker per backend
type BackendHealth struct {
	Failures      int
	LastFailure   time.Time
	Disabled      bool
	DisabledAt    time.Time
	CooldownUntil time.Time
}

// CircuitBreaker manages health state for multiple backends
type CircuitBreaker struct {
	health   map[string]*BackendHealth
	mu       sync.RWMutex
	callback CircuitBreakerCallback
}

// NewCircuitBreaker creates a new circuit breaker for the given backend names
func NewCircuitBreaker(backendNames []string, callback CircuitBreakerCallback) *CircuitBreaker {
	health := make(map[string]*BackendHealth)
	for _, name := range backendNames {
		health[name] = &BackendHealth{}
	}
	return &CircuitBreaker{health: health, callback: callback}
}

// IsHealthy checks if a backend should receive requests.
// If cooldown has passed, auto-recovers the backend.
func (cb *CircuitBreaker) IsHealthy(name string) bool {
	cb.mu.RLock()
	h := cb.health[name]
	if h == nil || !h.Disabled {
		cb.mu.RUnlock()
		return true
	}

	if time.Now().After(h.recoverAfter()) {
		cb.mu.RUnlock()
		// Auto-recover: upgrade to write lock and reset state
		cb.mu.Lock()
		// Re-check after acquiring write lock (double-check locking)
		if h.Disabled && time.Now().After(h.recoverAfter()) {
			h.Disabled = false
			h.Failures = 0
			h.CooldownUntil = time.Time{}
			if cb.callback != nil {
				cb.callback(CircuitBreakerEvent{
					Backend:   name,
					EventType: "recovered",
				})
			}
		}
		healthy := !h.Disabled
		cb.mu.Unlock()
		return healthy
	}

	cb.mu.RUnlock()
	return false
}

// RecordFailure tracks failures and disables backend if threshold reached
func (cb *CircuitBreaker) RecordFailure(name string, err error) {
	// A failed client construction during key rotation is not a backend
	// health signal — do not advance circuit-breaker state for it.
	if errors.Is(err, ErrKeyClientInit) || IsConsumerError(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	h := cb.health[name]
	if h == nil {
		return
	}

	h.Failures++
	h.LastFailure = time.Now()

	// Check for deterministic provider/model or quota/rate-limit errors.
	// Unsupported model IDs cannot recover by immediate retry; with catalog
	// expanded backends this quarantines the single provider/model candidate.
	isUnsupportedModel := IsModelUnsupportedError(err)
	isQuota := IsRateLimitError(err)

	if isUnsupportedModel || isQuota || h.Failures >= MaxConsecutiveFailures {
		if !h.Disabled {
			h.Disabled = true
			h.DisabledAt = h.LastFailure
			if isUnsupportedModel {
				h.CooldownUntil = h.LastFailure.Add(RateLimitCooldown)
			} else {
				h.CooldownUntil = cooldownUntilFromError(err, h.LastFailure)
			}
			if cb.callback != nil {
				cb.callback(CircuitBreakerEvent{
					Backend:   name,
					EventType: "disabled",
					Failures:  h.Failures,
					IsQuota:   isQuota,
				})
			}
		}
	}
}

// RecordSuccess resets failure count on success.
// A success during an active rate-limit cooldown does NOT re-enable the
// backend: if it was disabled with a quota-level failure count and the
// RateLimitCooldown has not yet elapsed, the backend stays quarantined.
func (cb *CircuitBreaker) RecordSuccess(name string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	h := cb.health[name]
	if h == nil {
		return
	}

	// Respect explicit provider reset metadata: a success seen during that
	// window must not clear quarantine early.
	if h.Disabled && !h.CooldownUntil.IsZero() && time.Now().Before(h.CooldownUntil) {
		return
	}

	// Respect an active rate-limit cooldown: do not clear quarantine early.
	if h.Disabled && h.Failures >= QuotaErrorThreshold && time.Since(h.DisabledAt) < RateLimitCooldown {
		return
	}

	// Reset on success
	wasDisabled := h.Disabled
	h.Failures = 0
	h.Disabled = false
	h.CooldownUntil = time.Time{}

	if wasDisabled && cb.callback != nil {
		cb.callback(CircuitBreakerEvent{
			Backend:   name,
			EventType: "recovered",
		})
	}
}

// GetHealth returns health information for a backend
func (cb *CircuitBreaker) GetHealth(name string) BackendHealth {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	h := cb.health[name]
	if h == nil {
		return BackendHealth{}
	}

	return BackendHealth{
		Failures:      h.Failures,
		LastFailure:   h.LastFailure,
		Disabled:      h.Disabled,
		DisabledAt:    h.DisabledAt,
		CooldownUntil: h.CooldownUntil,
	}
}

// GetAllHealth returns health information for all backends
func (cb *CircuitBreaker) GetAllHealth() map[string]BackendHealth {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	result := make(map[string]BackendHealth, len(cb.health))
	for name, h := range cb.health {
		result[name] = BackendHealth{
			Failures:      h.Failures,
			LastFailure:   h.LastFailure,
			Disabled:      h.Disabled,
			DisabledAt:    h.DisabledAt,
			CooldownUntil: h.CooldownUntil,
		}
	}
	return result
}

func (h *BackendHealth) recoverAfter() time.Time {
	if h == nil || !h.Disabled {
		return time.Time{}
	}
	if !h.CooldownUntil.IsZero() {
		return h.CooldownUntil
	}
	cooldown := FailureCooldown
	if h.Failures >= QuotaErrorThreshold {
		cooldown = RateLimitCooldown
	}
	return h.DisabledAt.Add(cooldown)
}

func cooldownUntilFromError(err error, now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	if d := RetryAfterFromError(err); d > 0 {
		return now.Add(d)
	}
	return time.Time{}
}
