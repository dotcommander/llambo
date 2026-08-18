package catalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecordQualityEvidenceStoresTaskScoreAndTag(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	cat := &Catalog{Version: 1, Providers: make(map[string]*ProviderCatalog)}

	err := RecordQualityEvidence(cat, QualityImportRecord{
		Provider: "openrouter",
		Model:    "qwen/qwen3-30b-a3b-instruct-2507",
		Task:     "Extraction",
		Score:    1,
		Source:   "distill 86-fact benchmark",
	}, now)
	require.NoError(t, err)

	entry := cat.Providers["openrouter"].Models["qwen/qwen3-30b-a3b-instruct-2507"]
	require.NotNil(t, entry)
	require.True(t, HasTag(entry, "extraction"))
	require.Equal(t, QualityEvidence{
		Score:     1,
		Source:    "distill 86-fact benchmark",
		UpdatedAt: now,
	}, entry.Quality["extraction"])
}

func TestRecordQualityEvidenceValidatesScore(t *testing.T) {
	t.Parallel()
	cat := &Catalog{Version: 1, Providers: make(map[string]*ProviderCatalog)}

	err := RecordQualityEvidence(cat, QualityImportRecord{
		Provider: "openrouter",
		Model:    "m",
		Task:     "extraction",
		Score:    1.1,
	}, time.Time{})
	require.Error(t, err)
}

func TestBestQualityEvidence(t *testing.T) {
	t.Parallel()
	entry := &ModelEntry{Quality: map[string]QualityEvidence{
		"chat":       {Score: 0.7},
		"extraction": {Score: 0.92},
	}}

	task, evidence, ok := BestQualityEvidence(entry)
	require.True(t, ok)
	require.Equal(t, "extraction", task)
	require.Equal(t, 0.92, evidence.Score)
}

func TestBestBenchmarkEvidenceUsesLatestMeasurement(t *testing.T) {
	t.Parallel()
	older := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	later := older.Add(time.Hour)
	entry := &ModelEntry{Benchmarks: map[string]BenchmarkEvidence{
		"older":  {LatencyMS: 100, UpdatedAt: older},
		"latest": {LatencyMS: 200, UpdatedAt: later},
	}}

	name, evidence, ok := BestBenchmarkEvidence(entry)
	require.True(t, ok)
	require.Equal(t, "latest", name)
	require.Equal(t, int64(200), evidence.LatencyMS)
}
