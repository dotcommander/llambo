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

// RecordPing records a lightweight health-check observation without extended
// latency metrics; extended timing fields on the observation are ignored.
func RecordPing(cat *Catalog, providerName, modelID string, observation HealthObservation) {
	observation.TTFB = 0
	observation.Generation = 0
	observation.SpeedTokensPerSecond = 0
	recordHealth(cat, providerName, modelID, observation, pingObservation)
}

// RecordPingWithMetrics records a health observation with TTFB, generation, and
// token-speed metrics.
func RecordPingWithMetrics(cat *Catalog, providerName, modelID string, observation HealthObservation) {
	recordHealth(cat, providerName, modelID, observation, pingObservation)
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

// ClassifyError maps an error string onto a stable category consumed by
// quarantine policy and reporting.
func ClassifyError(errText string) string {
	lower := strings.ToLower(strings.TrimSpace(errText))
	switch {
	case lower == "":
		return ""
	case containsAny(lower, "timeout", "deadline"):
		return "timeout"
	case containsAny(lower, "rate limit", "429"):
		return "rate_limit"
	case isModelAvailabilityError(lower):
		return "model_unsupported"
	case containsAny(lower, "api key", "unauthorized", "401", "403"):
		return "auth"
	default:
		return "error"
	}
}

// containsAny reports whether s contains any one of the substrings.
func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// isModelAvailabilityError matches model rejection wording: a mention of the
// model plus an availability-failure phrase.
func isModelAvailabilityError(lower string) bool {
	return strings.Contains(lower, "model") && containsAny(lower,
		"not found", "invalid", "unsupported", "no longer available", "end of life", "410 gone")
}
