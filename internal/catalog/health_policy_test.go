package catalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthObservationPolicies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	prior := now.Add(time.Hour)
	tests := []struct {
		name         string
		success      bool
		latency      time.Duration
		failures     int
		wantFailures int
		pingUntil    time.Time
		promptUntil  time.Time
	}{
		{"fast success", true, time.Second, 2, 0, time.Time{}, time.Time{}},
		{"threshold success", true, SlowPingThreshold, 2, 0, time.Time{}, time.Time{}},
		{"slow success", true, SlowPingThreshold + time.Nanosecond, 2, 0, now.Add(QuarantineDuration), prior},
		{"first failure", false, time.Second, 0, 1, prior, prior},
		{"second failure", false, time.Second, 1, 2, prior, prior},
		{"third failure", false, time.Second, 2, 3, now.Add(QuarantineDuration), now.Add(QuarantineDuration)},
		{"slow first failure", false, SlowPingThreshold + time.Nanosecond, 0, 1, now.Add(QuarantineDuration), now.Add(QuarantineDuration)},
	}
	for _, tt := range tests {
		for _, prompt := range []bool{false, true} {
			name := "ping"
			if prompt {
				name = "prompt"
			}
			t.Run(tt.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				entry := &ModelEntry{
					FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-time.Minute),
					Pinned: true, Avoid: true, Tags: []string{"retained"},
					FailureCount: tt.failures, QuarantineUntil: prior,
				}
				cat := &Catalog{Providers: map[string]*ProviderCatalog{
					"provider": {Models: map[string]*ModelEntry{"model": entry}},
				}}
				observation := HealthObservation{
					Success: tt.success, Latency: tt.latency, TTFB: 25 * time.Millisecond,
					Generation: 80 * time.Millisecond, SpeedTokensPerSecond: 12.5,
					TokensIn: 3, TokensOut: 7, CheckedAt: now,
				}
				if !tt.success {
					observation.Error = "deadline exceeded"
				}
				wantUntil := tt.pingUntil
				if prompt {
					RecordPrompt(cat, "provider", "model", observation)
					wantUntil = tt.promptUntil
				} else {
					RecordPingWithMetrics(cat, "provider", "model", observation.Success, observation.Latency,
						observation.TTFB, observation.Generation, observation.SpeedTokensPerSecond,
						observation.TokensIn, observation.TokensOut, observation.Error, observation.CheckedAt)
				}
				require.Equal(t, wantUntil, entry.QuarantineUntil)
				require.Equal(t, tt.wantFailures, entry.FailureCount)
				require.Equal(t, tt.success, entry.LastPing.Success)
				require.Equal(t, tt.latency.Milliseconds(), entry.LastPing.LatencyMS)
				require.Equal(t, int64(25), entry.LastPing.TTFBMS)
				require.Equal(t, int64(80), entry.LastPing.GenerationMS)
				require.Equal(t, 12.5, entry.LastPing.SpeedTokensPerSecond)
				require.Equal(t, 3, entry.LastPing.TokensIn)
				require.Equal(t, 7, entry.LastPing.TokensOut)
				require.Equal(t, now, entry.LastPing.CheckedAt)
				if !tt.success {
					require.Equal(t, "timeout", entry.LastPing.ErrorCategory)
					require.Equal(t, observation.Error, entry.LastPing.Error)
				}
				require.Equal(t, now.Add(-time.Hour), entry.FirstSeen)
				require.Equal(t, now.Add(-time.Minute), entry.LastSeen)
				require.True(t, entry.Pinned)
				require.True(t, entry.Avoid)
				require.Equal(t, []string{"retained"}, entry.Tags)
			})
		}
	}
}

func TestPromptObservationCreatesModelWithoutSlowQuarantine(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cat := &Catalog{Providers: map[string]*ProviderCatalog{}}
	RecordPrompt(cat, "provider", "model", HealthObservation{
		Success: true, Latency: SlowPingThreshold + time.Second, CheckedAt: now,
	})
	entry := cat.Providers["provider"].Models["model"]
	require.Equal(t, now, entry.FirstSeen)
	require.Equal(t, now, entry.LastSeen)
	require.True(t, entry.QuarantineUntil.IsZero())
}
