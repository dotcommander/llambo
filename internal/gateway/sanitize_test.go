package gateway

import (
	"testing"

	"github.com/dotcommander/llambo/providers"
	"github.com/stretchr/testify/require"
)

func TestSanitizeUpstreamError_StripsPII(t *testing.T) {
	t.Parallel()
	pii := "123-45-6789"
	err := providers.NewOpenAIError("all providers failed: openai: rate limit for 'SSN "+pii+"'", 429, nil)
	errType, msg := sanitizeUpstreamError(err)
	require.Equal(t, "rate_limit", errType)
	require.NotContains(t, msg, pii)
	require.NotContains(t, msg, "SSN")
	require.Equal(t, "Upstream provider rate limit exceeded", msg)
}
