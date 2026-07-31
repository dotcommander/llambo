package modelsdev

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSyncWritesNormalizedFile(t *testing.T) {
	t.Parallel()

	server := newSampleServer(t)
	outPath := filepath.Join(t.TempDir(), "models-dev-prices.json")

	res, next, err := RunSync(context.Background(), SyncOptions{
		ProviderToKey: sampleProviderToKey(),
		URL:           server.URL,
		OutPath:       outPath,
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 3, res.Providers)
	assert.Equal(t, 4, res.Models)
	assert.Equal(t, 4, res.Added)
	assert.Equal(t, 0, res.Changed)
	assert.Equal(t, 0, res.Removed)
	require.Len(t, next, 4)

	b, err := os.ReadFile(outPath)
	require.NoError(t, err)
	var written costs.ModelsDevFile
	require.NoError(t, json.Unmarshal(b, &written))
	require.Len(t, written, 4)
	assert.Contains(t, written, "gemini:gemini-2.5-pro")
	assert.Contains(t, written, "gemini:gemini-3.1-flash-lite")
}

func TestRunSyncDryRunDoesNotWrite(t *testing.T) {
	t.Parallel()

	server := newSampleServer(t)
	outPath := filepath.Join(t.TempDir(), "models-dev-prices.json")

	res, next, err := RunSync(context.Background(), SyncOptions{
		ProviderToKey: sampleProviderToKey(),
		URL:           server.URL,
		OutPath:       outPath,
		DryRun:        true,
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 4, res.Models)
	require.Len(t, next, 4)
	_, err = os.Stat(outPath)
	require.True(t, errors.Is(err, os.ErrNotExist), "expected dry run not to create %s", outPath)
}

func TestRunSyncDiffsPriorFile(t *testing.T) {
	t.Parallel()

	server := newSampleServer(t)
	outPath := filepath.Join(t.TempDir(), "models-dev-prices.json")
	prior := costs.ModelsDevFile{
		"openai:gpt-4o": {InputPer1M: 1, OutputPer1M: 1},
	}
	b, err := json.Marshal(prior)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(outPath, b, 0o644))

	res, next, err := RunSync(context.Background(), SyncOptions{
		ProviderToKey: sampleProviderToKey(),
		URL:           server.URL,
		OutPath:       outPath,
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 4, res.Models)
	assert.Equal(t, 3, res.Added)
	assert.Equal(t, 1, res.Changed)
	assert.Equal(t, 0, res.Removed)
	require.Len(t, next, 4)
}

func TestRunSyncFilterPreservesUnfilteredPriorProviders(t *testing.T) {
	t.Parallel()

	server := newSampleServer(t)
	outPath := filepath.Join(t.TempDir(), "models-dev-prices.json")
	prior := costs.ModelsDevFile{
		"openai:gpt-4o":   {InputPer1M: 99, OutputPer1M: 99},
		"gemini:old-gone": {InputPer1M: 1, OutputPer1M: 1},
	}
	b, err := json.Marshal(prior)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(outPath, b, 0o644))

	res, next, err := RunSync(context.Background(), SyncOptions{
		ProviderToKey: sampleProviderToKey(),
		Filter:        []string{"gemini"},
		URL:           server.URL,
		OutPath:       outPath,
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 3, res.Models)
	assert.Equal(t, 2, res.Added)
	assert.Equal(t, 0, res.Changed)
	assert.Equal(t, 1, res.Removed)
	require.Len(t, next, 3)
	assert.Equal(t, costs.ModelsDevPrice{InputPer1M: 99, OutputPer1M: 99}, next["openai:gpt-4o"])
	assert.Contains(t, next, "gemini:gemini-2.5-pro")
	assert.Contains(t, next, "gemini:gemini-3.1-flash-lite")
	assert.NotContains(t, next, "gemini:old-gone")
}

func newSampleServer(t *testing.T) *httptest.Server {
	t.Helper()

	data, err := os.ReadFile("testdata/sample.json")
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, writeErr := w.Write(data)
		assert.NoError(t, writeErr)
	}))
	t.Cleanup(server.Close)
	return server
}

func sampleProviderToKey() map[string]string {
	return map[string]string{"openai": "openai", "gemini": "google", "zai": "zai"}
}
