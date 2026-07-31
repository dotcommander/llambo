package providers

import (
	"net/http"
	"testing"
	"time"
)

func TestExtractRetryAfter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	httpDate := now.Add(90 * time.Second).Format(http.TimeFormat)

	tests := []struct {
		name string
		msg  string
		want time.Duration
	}{
		{
			name: "retry after seconds",
			msg:  "HTTP 429 Retry-After: 30",
			want: 30 * time.Second,
		},
		{
			name: "rate limit reset compact duration",
			msg:  "rate limited; x-ratelimit-reset-requests: 1m26.5s",
			want: 86500 * time.Millisecond,
		},
		{
			name: "retry after http date",
			msg:  "too many requests Retry-After: " + httpDate,
			want: 90 * time.Second,
		},
		{
			name: "missing",
			msg:  "rate limit exceeded",
			want: 0,
		},
		{
			name: "expired date",
			msg:  "Retry-After: " + now.Add(-time.Minute).Format(http.TimeFormat),
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExtractRetryAfter(tt.msg, now); got != tt.want {
				t.Fatalf("ExtractRetryAfter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenAIError_PreservesRetryAfter(t *testing.T) {
	t.Parallel()
	err := NewOpenAIErrorWithRetryAfter("rate limit", 429, 45*time.Second, nil)
	if got := RetryAfterFromError(err); got != 45*time.Second {
		t.Fatalf("RetryAfterFromError() = %v, want 45s", got)
	}
}
