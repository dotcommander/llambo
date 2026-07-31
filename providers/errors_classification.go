package providers

import (
	"errors"
	"strings"
	"time"
)

// ErrorCategory classifies errors for handling decisions
type ErrorCategory int

const (
	UnknownError ErrorCategory = iota
	TransientError
	RateLimitError
	QuotaError
	ConfigError
	AuthError
)

// String returns a human-readable name for the error category
func (c ErrorCategory) String() string {
	switch c {
	case TransientError:
		return "transient"
	case RateLimitError:
		return "rate_limit"
	case QuotaError:
		return "quota"
	case ConfigError:
		return "config"
	case AuthError:
		return "auth"
	default:
		return "unknown"
	}
}

// ClassifyError determines the category of an error for handling decisions.
// It uses HTTP status codes as the primary classification method when available,
// falling back to string pattern matching for errors without status codes.
func ClassifyError(err error) ErrorCategory {
	if err == nil {
		return UnknownError
	}

	// Try to get HTTP status code from OpenAIError (errors.As unwraps the chain)
	var oe *OpenAIError
	if errors.As(err, &oe) && oe.statusCode > 0 {
		if category := classifyByStatusCode(oe.statusCode); category != UnknownError {
			return category
		}
	}

	// Fall back to string pattern matching
	msg := strings.ToLower(err.Error())
	return classifyByPatterns(msg)
}

// classifyByStatusCode classifies errors based on HTTP status code.
// This is the preferred method as it's more reliable than string matching.
func classifyByStatusCode(code int) ErrorCategory {
	if containsInt(RateLimitStatusCodes, code) {
		return RateLimitError
	}
	if containsInt(AuthErrorStatusCodes, code) {
		return AuthError
	}
	if containsInt(TransientStatusCodes, code) {
		return TransientError
	}
	return UnknownError
}

// classifyByPatterns classifies errors using string pattern matching.
// Used as fallback when HTTP status code is not available.
func classifyByPatterns(msg string) ErrorCategory {
	// Check rate limit patterns first (most common actionable error)
	if containsAny(msg, RateLimitPatterns) {
		return RateLimitError
	}

	// Check quota patterns (similar to rate limit but longer recovery)
	if containsAny(msg, QuotaPatterns) {
		return QuotaError
	}

	// Check auth patterns (permanent, needs config fix)
	if containsAny(msg, AuthPatterns) {
		return AuthError
	}

	// Check config patterns (permanent, needs code/config fix)
	if containsAny(msg, ConfigPatterns) {
		return ConfigError
	}

	// Check transient patterns (worth retrying)
	if containsAny(msg, TransientPatterns) {
		return TransientError
	}

	// Also check for status codes embedded in error messages
	if code := extractHTTPStatus(msg); code > 0 {
		if category := classifyByStatusCode(code); category != UnknownError {
			return category
		}
	}

	return UnknownError
}

// IsRateLimitError checks if an error indicates rate limiting or quota exceeded.
// Exported for direct use in circuit breaker.
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	category := ClassifyError(err)
	return category == RateLimitError || category == QuotaError
}

// IsRetryable returns true if the error is worth retrying.
// Rate limit, quota, auth, and config errors are NOT retried - they hit circuit breaker.
func IsRetryable(err error) bool {
	return ClassifyError(err) == TransientError
}

// IsAuthError checks if an error indicates authentication/authorization failure.
func IsAuthError(err error) bool {
	return ClassifyError(err) == AuthError
}

// IsConfigError checks if an error indicates a configuration problem.
func IsConfigError(err error) bool {
	return ClassifyError(err) == ConfigError
}

// IsModelUnsupportedError reports deterministic model-ID failures. These are
// config errors, but routing can quarantine only the affected backend/model.
func IsModelUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	return containsAny(strings.ToLower(err.Error()), ModelUnsupportedPatterns)
}

// RetryAfterFromError returns provider-supplied retry/reset timing preserved in
// an OpenAIError wrapper, if available.
func RetryAfterFromError(err error) time.Duration {
	if err == nil {
		return 0
	}
	var oe *OpenAIError
	if errors.As(err, &oe) {
		return oe.RetryAfter()
	}
	return 0
}
