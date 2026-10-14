package providers

import "time"

// eligibleCanaryEvents preserves the promotion window and excludes incomplete streams.
func eligibleCanaryEvents(events []RouteEvent, startedAt string) []RouteEvent {
	start, _ := time.Parse(time.RFC3339, startedAt)
	eligible := make([]RouteEvent, 0, len(events))
	for _, event := range events {
		if event.PromotionIneligible || (!start.IsZero() && event.Timestamp.Before(start)) {
			continue
		}
		eligible = append(eligible, event)
	}
	return eligible
}

func mostUsedCanaryBaseline(events []RouteEvent, canary string) string {
	counts := make(map[string]int)
	for _, event := range events {
		if !event.IsCanary && event.ChosenProvider != "" && event.ChosenProvider != canary {
			counts[event.ChosenProvider]++
		}
	}
	baseline, maxCount := "", 0
	for provider, count := range counts {
		if count > maxCount {
			baseline, maxCount = provider, count
		}
	}
	return baseline
}

type canaryMetrics struct {
	total     int
	successes int
	latency   int64
	cost      float64
}

func collectCanaryMetrics(events []RouteEvent, provider string, isCanary bool) canaryMetrics {
	var metrics canaryMetrics
	for _, event := range events {
		if event.ChosenProvider != provider || event.IsCanary != isCanary {
			continue
		}
		metrics.total++
		if event.Success {
			metrics.successes++
		}
		metrics.latency += event.LatencyMs
		metrics.cost += event.CostUSD
	}
	return metrics
}

func (m canaryMetrics) averages() (successRate float64, latency int64, cost float64) {
	if m.total == 0 {
		return 0, 0, 0
	}
	return float64(m.successes) / float64(m.total), m.latency / int64(m.total), m.cost / float64(m.total)
}
