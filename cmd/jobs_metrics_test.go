package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/gateway"
	"github.com/stretchr/testify/require"
)

func TestJobMetricsAccumulateResults(t *testing.T) {
	t.Parallel()
	first := []gateway.JobResult{
		{Backend: "alpha", DurationMs: 10},
		{Backend: "", DurationMs: 0},
	}
	second := []gateway.JobResult{
		{Backend: "zeta", DurationMs: 30, Status: "failed"},
		{Backend: "alpha", DurationMs: -1},
	}

	tests := []struct {
		name string
		add  func(*jobRequestMetrics)
	}{
		{
			name: "one combined response",
			add: func(metrics *jobRequestMetrics) {
				metrics.addAll(append(append([]gateway.JobResult{}, first...), second...))
			},
		},
		{
			name: "repeated responses",
			add: func(metrics *jobRequestMetrics) {
				metrics.addAll(first)
				metrics.addAll(second)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			metrics := newJobRequestMetrics()
			tt.add(&metrics)
			require.Equal(t, map[string]int{"alpha": 2, "zeta": 1}, metrics.backendCounts)
			require.Equal(t, []time.Duration{10 * time.Millisecond, 30 * time.Millisecond}, metrics.latencies)
		})
	}
}

func TestCalculateStatsPreservesMetricsAndIntegrity(t *testing.T) {
	t.Parallel()
	resp := &gateway.JobResponse{
		Total:     3,
		Completed: 1,
		Failed:    2,
		Results: []gateway.JobResult{
			{ID: "known", Status: "failed", Backend: "zeta", DurationMs: 30},
			{ID: "known", Backend: "alpha", DurationMs: 10},
			{ID: "unknown", DurationMs: 0},
		},
	}
	stats := calculateStats(resp, []gateway.JobRequest{{ID: "known"}, {ID: "missing"}}, true)

	require.Equal(t, map[string]int{"alpha": 1, "zeta": 1}, stats.BackendCounts)
	require.Equal(t, []time.Duration{30 * time.Millisecond, 10 * time.Millisecond}, stats.Latencies)
	require.NotNil(t, stats.BackendCounts)
	require.NotNil(t, stats.Latencies)
	require.Equal(t, []string{"known", "known", "unknown"}, []string{resp.Results[0].ID, resp.Results[1].ID, resp.Results[2].ID})
	require.Contains(t, stats.IntegrityIssues, "duplicate ID: known")
	require.Contains(t, stats.IntegrityIssues, "unknown ID in results: unknown")
	require.Contains(t, stats.IntegrityIssues, "missing ID in results: missing")
	require.Contains(t, stats.IntegrityIssues, "result count mismatch: expected 2, got 3")

	empty := calculateStats(&gateway.JobResponse{}, nil, false)
	require.NotNil(t, empty.BackendCounts)
	require.NotNil(t, empty.Latencies)
	require.Empty(t, empty.BackendCounts)
	require.Empty(t, empty.Latencies)
}

func TestPrintJobResultsToRendersPercentilesAndSortsStatsInPlace(t *testing.T) {
	t.Parallel()
	latencies := make([]time.Duration, 20)
	for i := range latencies {
		latencies[i] = time.Duration(20-i) * time.Millisecond
	}
	stats := &JobStats{
		TotalRequests: 20,
		Completed:     20,
		Latencies:     latencies,
		BackendCounts: map[string]int{"zeta": 1, "alpha": 1},
	}
	var out bytes.Buffer
	printJobResultsTo(&out, &gateway.JobResponse{JobID: "batch", Status: "completed"}, stats)

	rendered := out.String()
	require.Contains(t, rendered, "Latency")
	require.Contains(t, rendered, "P50:     11ms")
	require.Contains(t, rendered, "P95:     20ms")
	require.Contains(t, rendered, "P99:     20ms")
	require.Contains(t, rendered, "(  5.0%)")
	require.Less(t, strings.Index(rendered, "alpha"), strings.Index(rendered, "zeta"))
	require.Equal(t, 1*time.Millisecond, stats.Latencies[0])
	require.Equal(t, 20*time.Millisecond, stats.Latencies[len(stats.Latencies)-1])
}

func TestPrintJobResultsToHandlesEmptyAndSingleMetrics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		stats     *JobStats
		contains  string
		notExists []string
	}{
		{
			name:      "empty metrics omit metric sections",
			stats:     &JobStats{BackendCounts: make(map[string]int), Latencies: make([]time.Duration, 0)},
			notExists: []string{"Latency", "Backend Distribution"},
		},
		{
			name:     "single latency supplies every percentile",
			stats:    &JobStats{Latencies: []time.Duration{7 * time.Millisecond}, BackendCounts: make(map[string]int)},
			contains: "P99:     7ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			printJobResultsTo(&out, &gateway.JobResponse{Status: "completed"}, tt.stats)
			if tt.contains != "" {
				require.Contains(t, out.String(), tt.contains)
			}
			for _, text := range tt.notExists {
				require.NotContains(t, out.String(), text)
			}
		})
	}
}

func TestPrintStressResultsToUsesSuccessfulJobsAndCompletedDenominator(t *testing.T) {
	t.Parallel()
	results := []stressJobResult{
		{
			response: &gateway.JobResponse{
				Status: "completed", Completed: 1,
				Results: []gateway.JobResult{{Backend: "alpha", DurationMs: 10}},
			},
			duration: time.Second,
		},
		{
			response: &gateway.JobResponse{
				Status: "completed", Completed: 1,
				Results: []gateway.JobResult{{Backend: "zeta", DurationMs: 20}},
			},
			duration: time.Second,
		},
		{
			err:      errors.New("job failed"),
			response: &gateway.JobResponse{Results: []gateway.JobResult{{Backend: "ignored", DurationMs: 99}}},
		},
	}
	var out bytes.Buffer
	printStressResultsTo(&out, results, 2*time.Second, 10)

	rendered := out.String()
	require.Contains(t, rendered, "Total jobs:        3 (2 successful)")
	require.Contains(t, rendered, "Latency (across all requests)")
	require.Contains(t, rendered, "P50:     20ms")
	require.Contains(t, rendered, "( 50.0%)")
	require.NotContains(t, rendered, "ignored")
	require.Less(t, strings.Index(rendered, "alpha"), strings.Index(rendered, "zeta"))
}
