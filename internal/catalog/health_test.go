package catalog

import (
	"testing"
	"time"
)

func TestRecordPingWithMetricsPersistsGenerationSpeed(t *testing.T) {
	now := time.Now().UTC()
	cat := &Catalog{Providers: map[string]*ProviderCatalog{}}

	RecordPingWithMetrics(cat, "openai", "gpt-5", HealthObservation{
		Success: true, Latency: 2 * time.Second, TTFB: 800 * time.Millisecond,
		Generation: 1200 * time.Millisecond, SpeedTokensPerSecond: 25,
		TokensIn: 4, TokensOut: 30, CheckedAt: now,
	})

	got := cat.Providers["openai"].Models["gpt-5"].LastPing
	if got.LatencyMS != 2000 || got.TTFBMS != 800 || got.GenerationMS != 1200 {
		t.Fatalf("timings = latency %d, ttfb %d, generation %d; want 2000, 800, 1200", got.LatencyMS, got.TTFBMS, got.GenerationMS)
	}
	if got.SpeedTokensPerSecond != 25 {
		t.Fatalf("speed = %v, want 25", got.SpeedTokensPerSecond)
	}
}
