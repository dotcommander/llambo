package providers

import (
	"fmt"
	"hash/fnv"
	"time"
)

// CanaryConfig controls canary routing: send a percentage of traffic to a trial provider.
type CanaryConfig struct {
	Provider     string  `json:"provider"`                // canary provider name (must exist in providers config)
	TrafficPct   float64 `json:"traffic_pct"`             // 0.0-1.0 fraction of requests routed to canary
	PromoteAfter int     `json:"promote_after,omitempty"` // promote after N canary requests (0 = manual only)
	Baseline     string  `json:"baseline,omitempty"`      // baseline provider to compare against (default: top scorer)
	StartedAt    string  `json:"started_at,omitempty"`    // RFC3339 timestamp when canary started
}

// PromoteCanaryConfig applies the existing in-memory config transition for a
// canary promotion. The supplied value identifies the provider and baseline
// captured by the caller; clearing the currently loaded route is part of the
// compatibility contract shared by manual and automatic promotion.
//
// Loading, saving, evaluation, and promotion policy remain with callers.
func PromoteCanaryConfig(cfg *GlobalConfig, canary CanaryConfig) (string, int) {
	targetPriority := 1
	if canary.Baseline != "" {
		if baseline, ok := cfg.Providers[canary.Baseline]; ok {
			targetPriority = baseline.Priority
		}
	} else {
		for _, provider := range cfg.Providers {
			if provider.Enabled && provider.Priority > 0 && provider.Priority < targetPriority {
				targetPriority = provider.Priority
			}
		}
	}

	if provider, ok := cfg.Providers[canary.Provider]; ok {
		provider.Priority = targetPriority
		cfg.Providers[canary.Provider] = provider
	}
	cfg.Routing.Canary = nil

	return canary.Provider, targetPriority
}

// shouldRouteToCanary returns true if this request should go to the canary provider.
// Uses FNV hash of request content for deterministic per-request routing.
func shouldRouteToCanary(canary *CanaryConfig, systemPrompt, userContent string) bool {
	if canary == nil || canary.Provider == "" || canary.TrafficPct <= 0 {
		return false
	}
	if canary.TrafficPct >= 1.0 {
		return true
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(systemPrompt))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(userContent))
	frac := float64(h.Sum64()%10000) / 10000.0
	return frac < canary.TrafficPct
}

// CanaryStatus summarizes canary vs baseline performance.
type CanaryStatus struct {
	CanaryProvider       string  `json:"canary_provider"`
	BaselineProvider     string  `json:"baseline_provider"`
	TrafficPct           float64 `json:"traffic_pct"`
	PromoteAfter         int     `json:"promote_after"`
	CanaryRequests       int     `json:"canary_requests"`
	BaselineRequests     int     `json:"baseline_requests"`
	CanarySuccessRate    float64 `json:"canary_success_rate"`
	BaselineSuccessRate  float64 `json:"baseline_success_rate"`
	CanaryAvgLatencyMs   int64   `json:"canary_avg_latency_ms"`
	BaselineAvgLatencyMs int64   `json:"baseline_avg_latency_ms"`
	CanaryAvgCostUSD     float64 `json:"canary_avg_cost_usd"`
	BaselineAvgCostUSD   float64 `json:"baseline_avg_cost_usd"`
	ShouldPromote        bool    `json:"should_promote"`
	PromoteReason        string  `json:"promote_reason,omitempty"`
	RejectReason         string  `json:"reject_reason,omitempty"`
}

// EvaluateCanary compares canary vs baseline metrics from route events.
func EvaluateCanary(canary *CanaryConfig, events []RouteEvent, configs map[string]Config) *CanaryStatus {
	if canary == nil || canary.Provider == "" {
		return nil
	}

	baseline := canary.Baseline
	status := &CanaryStatus{
		CanaryProvider:   canary.Provider,
		BaselineProvider: baseline,
		TrafficPct:       canary.TrafficPct,
		PromoteAfter:     canary.PromoteAfter,
	}

	var canarySuccesses, canaryTotal int
	var canaryLatency int64
	var canaryCost float64
	var baselineSuccesses, baselineTotal int
	var baselineLatency int64
	var baselineCost float64

	var startFilter time.Time
	if canary.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339, canary.StartedAt); err == nil {
			startFilter = t
		}
	}

	for _, ev := range events {
		if ev.PromotionIneligible {
			continue
		}
		if !startFilter.IsZero() && ev.Timestamp.Before(startFilter) {
			continue
		}
		if ev.IsCanary && ev.ChosenProvider == canary.Provider {
			canaryTotal++
			if ev.Success {
				canarySuccesses++
			}
			canaryLatency += ev.LatencyMs
			canaryCost += ev.CostUSD
		}
		if baseline != "" && ev.ChosenProvider == baseline && !ev.IsCanary {
			baselineTotal++
			if ev.Success {
				baselineSuccesses++
			}
			baselineLatency += ev.LatencyMs
			baselineCost += ev.CostUSD
		}
	}

	// If no explicit baseline, find the most-used non-canary provider
	if baseline == "" {
		counts := make(map[string]int)
		for _, ev := range events {
			if ev.PromotionIneligible {
				continue
			}
			if !startFilter.IsZero() && ev.Timestamp.Before(startFilter) {
				continue
			}
			if !ev.IsCanary && ev.ChosenProvider != "" && ev.ChosenProvider != canary.Provider {
				counts[ev.ChosenProvider]++
			}
		}
		maxCount := 0
		for name, c := range counts {
			if c > maxCount {
				maxCount = c
				baseline = name
			}
		}
		status.BaselineProvider = baseline
		for _, ev := range events {
			if ev.PromotionIneligible {
				continue
			}
			if !startFilter.IsZero() && ev.Timestamp.Before(startFilter) {
				continue
			}
			if ev.ChosenProvider == baseline && !ev.IsCanary {
				baselineTotal++
				if ev.Success {
					baselineSuccesses++
				}
				baselineLatency += ev.LatencyMs
				baselineCost += ev.CostUSD
			}
		}
	}

	status.CanaryRequests = canaryTotal
	status.BaselineRequests = baselineTotal
	if canaryTotal > 0 {
		status.CanarySuccessRate = float64(canarySuccesses) / float64(canaryTotal)
		status.CanaryAvgLatencyMs = canaryLatency / int64(canaryTotal)
		status.CanaryAvgCostUSD = canaryCost / float64(canaryTotal)
	}
	if baselineTotal > 0 {
		status.BaselineSuccessRate = float64(baselineSuccesses) / float64(baselineTotal)
		status.BaselineAvgLatencyMs = baselineLatency / int64(baselineTotal)
		status.BaselineAvgCostUSD = baselineCost / float64(baselineTotal)
	}

	status.ShouldPromote, status.PromoteReason, status.RejectReason = evaluatePromotionCriteria(status, canary.PromoteAfter)
	return status
}

func evaluatePromotionCriteria(s *CanaryStatus, promoteAfter int) (bool, string, string) {
	if promoteAfter > 0 && s.CanaryRequests < promoteAfter {
		return false, "", fmt.Sprintf("canary has %d/%d required requests", s.CanaryRequests, promoteAfter)
	}
	if s.CanaryRequests < 5 {
		return false, "", "insufficient canary data (< 5 requests)"
	}
	if s.BaselineRequests < 5 {
		return false, "", "insufficient baseline data (< 5 requests)"
	}
	if s.CanarySuccessRate < s.BaselineSuccessRate-0.02 {
		return false, "", "canary success rate too low"
	}
	if s.BaselineAvgLatencyMs > 0 && s.CanaryAvgLatencyMs > s.BaselineAvgLatencyMs*3/2 {
		return false, "", "canary latency exceeds 1.5x baseline"
	}
	if s.BaselineAvgCostUSD > 0 && s.CanaryAvgCostUSD > s.BaselineAvgCostUSD*2 {
		return false, "", "canary cost exceeds 2x baseline"
	}
	return true, "canary meets all promotion criteria", ""
}
