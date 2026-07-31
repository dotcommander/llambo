package providers

import (
	"errors"
	"testing"
)

func TestClassifyError_Nil(t *testing.T) {
	t.Parallel()
	if got := ClassifyError(nil); got != UnknownError {
		t.Errorf("ClassifyError(nil) = %v, want UnknownError", got)
	}
}

func TestClassifyError_RateLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		errMsg  string
		want    ErrorCategory
		isRate  bool
		isRetry bool
	}{
		// Rate limit patterns
		{"rate limit space", "rate limit exceeded", RateLimitError, true, false},
		{"rate_limit underscore", "rate_limit_exceeded", RateLimitError, true, false},
		{"too many requests", "too many requests", RateLimitError, true, false},
		{"throttled", "request throttled", RateLimitError, true, false},
		{"throttling", "throttling enabled", RateLimitError, true, false},

		// Quota patterns
		{"quota exceeded", "quota exceeded for today", QuotaError, true, false},
		{"current quota", "exceeded your current quota", QuotaError, true, false},
		{"billing", "billing issue detected", QuotaError, true, false},
		{"insufficient_quota", "insufficient_quota error", QuotaError, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := errors.New(tt.errMsg)
			got := ClassifyError(err)
			if got != tt.want {
				t.Errorf("ClassifyError(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
			if IsRateLimitError(err) != tt.isRate {
				t.Errorf("IsRateLimitError(%q) = %v, want %v", tt.errMsg, !tt.isRate, tt.isRate)
			}
			if IsRetryable(err) != tt.isRetry {
				t.Errorf("IsRetryable(%q) = %v, want %v", tt.errMsg, !tt.isRetry, tt.isRetry)
			}
		})
	}
}

func TestClassifyError_Auth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		errMsg string
	}{
		{"invalid api key", "invalid api key provided"},
		{"incorrect api key", "incorrect api key"},
		{"authentication failed", "authentication failed"},
		{"unauthorized", "unauthorized access"},
		{"api key not found", "api key not found"},
		{"invalid_api_key", "error: invalid_api_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := errors.New(tt.errMsg)
			if got := ClassifyError(err); got != AuthError {
				t.Errorf("ClassifyError(%q) = %v, want AuthError", tt.errMsg, got)
			}
			if IsAuthError(err) != true {
				t.Errorf("IsAuthError(%q) = false, want true", tt.errMsg)
			}
			// Auth errors should NOT be retryable
			if IsRetryable(err) {
				t.Errorf("IsRetryable(%q) = true, want false", tt.errMsg)
			}
		})
	}
}

func TestClassifyError_Config(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		errMsg string
	}{
		{"unsupported value", "unsupported value for parameter"},
		{"unsupported parameter", "unsupported parameter: foo"},
		{"invalid model", "invalid model specified"},
		{"model not found", "model not found"},
		{"does not exist", "resource does not exist"},
		{"invalid_model", "error: invalid_model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := errors.New(tt.errMsg)
			if got := ClassifyError(err); got != ConfigError {
				t.Errorf("ClassifyError(%q) = %v, want ConfigError", tt.errMsg, got)
			}
			if IsConfigError(err) != true {
				t.Errorf("IsConfigError(%q) = false, want true", tt.errMsg)
			}
			// Config errors should NOT be retryable
			if IsRetryable(err) {
				t.Errorf("IsRetryable(%q) = true, want false", tt.errMsg)
			}
		})
	}
}

func TestIsModelUnsupportedError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		errMsg string
		want   bool
	}{
		{"invalid model specified", true},
		{"model_not_found: unknown model", true},
		{"model is not supported for this endpoint", true},
		{"unsupported parameter: stop", false},
		{"rate limit exceeded", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.errMsg, func(t *testing.T) {
			t.Parallel()
			if got := IsModelUnsupportedError(errors.New(tt.errMsg)); got != tt.want {
				t.Fatalf("IsModelUnsupportedError(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}

func TestClassifyError_Transient(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		errMsg string
	}{
		{"server closed", "server closed connection unexpectedly"},
		{"connection reset", "connection reset by peer"},
		{"connection refused", "connection refused"},
		{"eof", "unexpected eof"},
		{"temporary failure", "temporary failure in name resolution"},
		{"i/o timeout", "i/o timeout"},
		{"tls handshake", "tls handshake timeout"},
		{"context deadline", "context deadline exceeded"},
		{"no such host", "no such host"},
		{"network unreachable", "network is unreachable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := errors.New(tt.errMsg)
			if got := ClassifyError(err); got != TransientError {
				t.Errorf("ClassifyError(%q) = %v, want TransientError", tt.errMsg, got)
			}
			// Transient errors SHOULD be retryable
			if !IsRetryable(err) {
				t.Errorf("IsRetryable(%q) = false, want true", tt.errMsg)
			}
		})
	}
}

func TestClassifyError_StatusCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		errMsg string
		want   ErrorCategory
	}{
		// Status code in error message
		{"429 in message", "status: 429 too many requests", RateLimitError},
		{"401 in message", "status 401 unauthorized", AuthError},
		{"403 in message", "http 403 forbidden", AuthError},
		{"502 in message", "bad gateway 502", TransientError},
		{"503 in message", "service unavailable 503", TransientError},
		{"504 in message", "gateway timeout 504", TransientError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := errors.New(tt.errMsg)
			if got := ClassifyError(err); got != tt.want {
				t.Errorf("ClassifyError(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}

func TestExtractHTTPStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  string
		want int
	}{
		{"bare status", "429", 429},
		{"status prefix", "status 429", 429},
		{"status colon", "status: 429", 429},
		{"status_code", "status_code: 401", 401},
		{"no status", "some error", 0},
		{"invalid number", "status: abc", 0},
		{"out of range", "status: 999", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := extractHTTPStatus(tt.msg); got != tt.want {
				t.Errorf("extractHTTPStatus(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestOpenAIError_StatusCode(t *testing.T) {
	t.Parallel()
	// Test with embedded status code
	oe := NewOpenAIError("rate limit exceeded", 429, nil)

	if got := oe.StatusCode(); got != 429 {
		t.Errorf("StatusCode() = %v, want 429", got)
	}

	// Classification should use status code
	if got := ClassifyError(oe); got != RateLimitError {
		t.Errorf("ClassifyError(OpenAIError with 429) = %v, want RateLimitError", got)
	}
}

func TestErrorCategory_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cat  ErrorCategory
		want string
	}{
		{UnknownError, "unknown"},
		{TransientError, "transient"},
		{RateLimitError, "rate_limit"},
		{QuotaError, "quota"},
		{ConfigError, "config"},
		{AuthError, "auth"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.cat.String(); got != tt.want {
				t.Errorf("ErrorCategory(%d).String() = %q, want %q", tt.cat, got, tt.want)
			}
		})
	}
}

func TestIsRateLimitError_IncludesQuota(t *testing.T) {
	t.Parallel()
	// IsRateLimitError should return true for both rate limit AND quota errors
	// because circuit breaker treats them the same way
	rateLimitErr := errors.New("rate limit exceeded")
	quotaErr := errors.New("quota exhausted")

	if !IsRateLimitError(rateLimitErr) {
		t.Error("IsRateLimitError should return true for rate limit errors")
	}
	if !IsRateLimitError(quotaErr) {
		t.Error("IsRateLimitError should return true for quota errors")
	}
}

func TestCaseInsensitivity(t *testing.T) {
	t.Parallel()
	// Error classification should be case-insensitive
	tests := []struct {
		errMsg string
		want   ErrorCategory
	}{
		{"RATE LIMIT exceeded", RateLimitError},
		{"Rate Limit Exceeded", RateLimitError},
		{"QUOTA EXCEEDED", QuotaError},
		{"Authentication Failed", AuthError},
		{"CONNECTION RESET", TransientError},
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			t.Parallel()
			err := errors.New(tt.errMsg)
			if got := ClassifyError(err); got != tt.want {
				t.Errorf("ClassifyError(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}
