package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvalFetchOptionsReadsExternalSourceKeys(t *testing.T) {
	t.Setenv("AA_API_KEY", "aa-secret")
	t.Setenv("LLM_STATS_KEY", "llm-stats-secret")
	options := evalFetchOptions(t.TempDir())
	require.Equal(t, "aa-secret", options.AAAPIKey)
	require.Equal(t, "llm-stats-secret", options.LLMStatsAPIKey)
}
