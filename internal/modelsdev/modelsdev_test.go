package modelsdev

import (
	"os"
	"testing"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/sample.json")
	require.NoError(t, err)

	api, err := Parse(data)
	require.NoError(t, err)
	assert.Contains(t, api, "openai")
	assert.Contains(t, api, "google")
	assert.Contains(t, api, "zai")
}

func TestNormalizeAllProviders(t *testing.T) {
	t.Parallel()

	api := parseSample(t)
	providerToKey := map[string]string{"openai": "openai", "gemini": "google", "zai": "zai"}

	got := Normalize(api, providerToKey, nil)

	require.Len(t, got, 4)
	assert.Equal(t, costs.ModelsDevPrice{InputPer1M: 2.5, OutputPer1M: 10}, got["openai:gpt-4o"])
	assert.Equal(t, costs.ModelsDevPrice{InputPer1M: 1.25, OutputPer1M: 10}, got["gemini:gemini-2.5-pro"])
	assert.Equal(t, costs.ModelsDevPrice{InputPer1M: 0.25, OutputPer1M: 1.50}, got["gemini:gemini-3.1-flash-lite"])
	assert.Equal(t, costs.ModelsDevPrice{InputPer1M: 1.2, OutputPer1M: 4}, got["zai:glm-5-turbo"])
	assert.NotContains(t, got, "gemini:gemini-no-price")
}

func TestNormalizeFilter(t *testing.T) {
	t.Parallel()

	api := parseSample(t)
	providerToKey := map[string]string{"openai": "openai", "gemini": "google", "zai": "zai"}

	got := Normalize(api, providerToKey, []string{"gemini"})

	require.Len(t, got, 2)
	assert.Equal(t, costs.ModelsDevPrice{InputPer1M: 1.25, OutputPer1M: 10}, got["gemini:gemini-2.5-pro"])
	assert.Equal(t, costs.ModelsDevPrice{InputPer1M: 0.25, OutputPer1M: 1.50}, got["gemini:gemini-3.1-flash-lite"])
}

func TestNormalizeMissingProvider(t *testing.T) {
	t.Parallel()

	api := parseSample(t)

	got := Normalize(api, map[string]string{"nope": "absent"}, nil)

	assert.Empty(t, got)
}

func parseSample(t *testing.T) map[string]ProviderModels {
	t.Helper()

	data, err := os.ReadFile("testdata/sample.json")
	require.NoError(t, err)

	api, err := Parse(data)
	require.NoError(t, err)
	return api
}
