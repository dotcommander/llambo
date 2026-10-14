package catalog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyErrorBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		errText string
		want    string
	}{
		{"", ""},
		{"   ", ""},
		{"something broke", "error"},
		{"model", "error"},
		{"invalid request", "error"}, // availability wording requires a model mention
		{"410 gone", "error"},        // same for the gone status without a model mention
		{"request TIMEOUT", "timeout"},
		{"context deadline exceeded", "timeout"},
		{"HTTP 429 Too Many Requests", "rate_limit"},
		{"rate limit exceeded", "rate_limit"},
		{"model not found", "model_unsupported"},
		{"model is no longer available", "model_unsupported"},
		{"model reached end of life (410 gone)", "model_unsupported"},
		{"403 Forbidden", "auth"},
		{"invalid API key", "auth"}, // no model mention, so auth wins over availability wording
	}

	for _, tt := range tests {
		require.Equal(t, tt.want, ClassifyError(tt.errText), "ClassifyError(%q)", tt.errText)
	}
}
