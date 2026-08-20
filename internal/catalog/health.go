package catalog

import (
	"strings"
	"time"
)

func RecordPing(cat *Catalog, providerName, modelID string, success bool, latency time.Duration, tokensIn, tokensOut int, errText string, checkedAt time.Time) {
	RecordPingWithMetrics(cat, providerName, modelID, success, latency, 0, 0, 0, tokensIn, tokensOut, errText, checkedAt)
}

func RecordPingWithMetrics(cat *Catalog, providerName, modelID string, success bool, latency, ttfb, generation time.Duration, speedTokensPerSecond float64, tokensIn, tokensOut int, errText string, checkedAt time.Time) {
	if cat == nil {
		return
	}
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
	}
	pc := cat.Providers[providerName]
	if pc == nil {
		pc = &ProviderCatalog{Models: make(map[string]*ModelEntry)}
		cat.Providers[providerName] = pc
	}
	if pc.Models == nil {
		pc.Models = make(map[string]*ModelEntry)
	}
	entry := pc.Models[modelID]
	if entry == nil {
		entry = &ModelEntry{FirstSeen: checkedAt, LastSeen: checkedAt}
		pc.Models[modelID] = entry
	}

	entry.LastPing = PingState{
		Success:              success,
		LatencyMS:            latency.Milliseconds(),
		TTFBMS:               ttfb.Milliseconds(),
		GenerationMS:         generation.Milliseconds(),
		SpeedTokensPerSecond: speedTokensPerSecond,
		ErrorCategory:        ClassifyError(errText),
		Error:                errText,
		TokensIn:             tokensIn,
		TokensOut:            tokensOut,
		CheckedAt:            checkedAt,
	}
	if success {
		entry.FailureCount = 0
		if latency <= SlowPingThreshold {
			entry.QuarantineUntil = time.Time{}
		}
	} else {
		entry.FailureCount++
	}
	if (!success && entry.FailureCount >= 3) || latency > SlowPingThreshold {
		entry.QuarantineUntil = checkedAt.Add(QuarantineDuration)
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
