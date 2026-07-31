package providers

import "time"

// Request configuration constants
const (
	// DefaultRequestTimeout is the maximum duration for a single LLM request
	DefaultRequestTimeout = 120 * time.Second

	// DefaultRetryCount is the number of retry attempts for transient errors
	DefaultRetryCount = 2
)

// DefaultRetryBackoffs is the full-jitter backoff schedule for transient errors:
// the first entry is the base unit and the last entry is the cap. Attempt i waits
// a random draw in [0, min(cap, base*2^i)] (see backoffDelay).
var DefaultRetryBackoffs = []time.Duration{1 * time.Second, 2 * time.Second}

// Circuit breaker constants
const (
	// RateLimitCooldown is how long to wait before retrying after rate limit errors
	RateLimitCooldown = 5 * time.Minute

	// FailureCooldown is how long to wait before retrying after consecutive failures
	FailureCooldown = 60 * time.Second

	// MaxConsecutiveFailures is the threshold that triggers circuit breaker
	MaxConsecutiveFailures = 3

	// QuotaErrorThreshold is the failure count that indicates a likely quota error.
	// When failures exceed this threshold, rate limit cooldown is used instead of failure cooldown.
	QuotaErrorThreshold = 10
)

// Default max tokens constant
const (
	// DefaultMaxTokens is the fallback max tokens when no provider config specifies it
	DefaultMaxTokens = 8000
)
