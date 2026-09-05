package catalog

import (
	"strings"
	"time"
)

// HealthObservation records timings before millisecond serialization so policy
// thresholds retain the precision of the original measurement.
type HealthObservation struct {
	Success              bool
	Latency              time.Duration
	TTFB                 time.Duration
	Generation           time.Duration
	SpeedTokensPerSecond float64
	TokensIn             int
	TokensOut            int
	Error                string
	CheckedAt            time.Time
}

type observationPolicy uint8

const (
	pingObservation observationPolicy = iota
	promptObservation
)

func RecordPing(cat *Catalog, providerName, modelID string, success bool, latency time.Duration, tokensIn, tokensOut int, errText string, checkedAt time.Time) {
	RecordPingWithMetrics(cat, providerName, modelID, success, latency, 0, 0, 0, tokensIn, tokensOut, errText, checkedAt)
}

func RecordPingWithMetrics(cat *Catalog, providerName, modelID string, success bool, latency, ttfb, generation time.Duration, speedTokensPerSecond float64, tokensIn, tokensOut int, errText string, checkedAt time.Time) {
	recordHealth(cat, providerName, modelID, HealthObservation{
		Success: success, Latency: latency, TTFB: ttfb, Generation: generation,
		SpeedTokensPerSecond: speedTokensPerSecond, TokensIn: tokensIn, TokensOut: tokensOut,
		Error: errText, CheckedAt: checkedAt,
	}, pingObservation)
}

// RecordPrompt preserves quarantine on slow successful generations: their
// duration reflects the workload, unlike a lightweight health ping.
func RecordPrompt(cat *Catalog, providerName, modelID string, observation HealthObservation) {
	recordHealth(cat, providerName, modelID, observation, promptObservation)
}

func recordHealth(cat *Catalog, providerName, modelID string, observation HealthObservation, policy observationPolicy) {
	if cat == nil {
		return
	}
	if observation.CheckedAt.IsZero() {
		observation.CheckedAt = time.Now().UTC()
	}
	pc := ensureProviderCatalog(cat, providerName)
	entry := ensureModelEntry(pc, modelID, observation.CheckedAt)

	entry.LastPing = PingState{
		Success:              observation.Success,
		LatencyMS:            observation.Latency.Milliseconds(),
		TTFBMS:               observation.TTFB.Milliseconds(),
		GenerationMS:         observation.Generation.Milliseconds(),
		SpeedTokensPerSecond: observation.SpeedTokensPerSecond,
		ErrorCategory:        ClassifyError(observation.Error),
		Error:                observation.Error,
		TokensIn:             observation.TokensIn,
		TokensOut:            observation.TokensOut,
		CheckedAt:            observation.CheckedAt,
	}
	slow := observation.Latency > SlowPingThreshold
	if observation.Success {
		entry.FailureCount = 0
		if !slow {
			entry.QuarantineUntil = time.Time{}
		}
	} else {
		entry.FailureCount++
	}
	if (!observation.Success && entry.FailureCount >= 3) || (slow && (!observation.Success || policy == pingObservation)) {
		entry.QuarantineUntil = observation.CheckedAt.Add(QuarantineDuration)
	}
}

func ClassifyError(errText string) string {
	lower := strings.ToLower(strings.TrimSpace(errText))
	if lower == "" {
		return ""
	}
	switch {
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return "timeout"
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "429"):
		return "rate_limit"
	case strings.Contains(lower, "model") && (strings.Contains(lower, "not found") || strings.Contains(lower, "invalid") || strings.Contains(lower, "unsupported") || strings.Contains(lower, "no longer available") || strings.Contains(lower, "end of life") || strings.Contains(lower, "410 gone")):
		return "model_unsupported"
	case strings.Contains(lower, "api key") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "401") || strings.Contains(lower, "403"):
		return "auth"
	default:
		return "error"
	}
}
