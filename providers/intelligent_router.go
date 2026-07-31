package providers

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type RoutingIntent string

type RoutingMode string

const (
	IntentChat       RoutingIntent = "chat"
	IntentCode       RoutingIntent = "code"
	IntentExtraction RoutingIntent = "extraction"
	IntentLongCtx    RoutingIntent = "long_context"
	IntentEmbeddings RoutingIntent = "embeddings"

	RoutingModeFastest  RoutingMode = "fastest"
	RoutingModeCheapest RoutingMode = "cheapest"
	RoutingModeBalanced RoutingMode = "balanced"
	RoutingModeQuality  RoutingMode = "quality"
)

type CandidateScore struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Score    float64 `json:"score"`
	Reason   string  `json:"reason"`
}

type RouteDecision struct {
	Mode       string           `json:"mode"`
	Intent     RoutingIntent    `json:"intent"`
	Chosen     string           `json:"chosen"`
	Reason     string           `json:"reason"`
	Candidates []CandidateScore `json:"candidates,omitempty"`
	IsCanary   bool             `json:"is_canary,omitempty"`
}

func DetectIntent(systemPrompt, userContent string) RoutingIntent {
	text := strings.ToLower(systemPrompt + "\n" + userContent)
	if strings.Contains(text, "```") || strings.Contains(text, "function") || strings.Contains(text, "golang") || strings.Contains(text, "python") || strings.Contains(text, "typescript") || strings.Contains(text, "debug") {
		return IntentCode
	}
	if strings.Contains(text, "extract") || strings.Contains(text, "summary") || strings.Contains(text, "evidence") {
		return IntentExtraction
	}
	if len(userContent) > 8000 {
		return IntentLongCtx
	}
	return IntentChat
}

func EstimatePromptTokens(systemPrompt, userContent string) int {
	return (len(systemPrompt) + len(userContent)) / 4
}

func NormalizeRoutingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(RoutingModeFastest):
		return string(RoutingModeFastest)
	case string(RoutingModeCheapest):
		return string(RoutingModeCheapest)
	case string(RoutingModeQuality):
		return string(RoutingModeQuality)
	default:
		return string(RoutingModeBalanced)
	}
}

func ResolveDailyLimitInt64(limits map[string]int64, provider string) (int64, bool) {
	return resolveDailyLimit(limits, provider)
}

func ResolveDailyLimitFloat64(limits map[string]float64, provider string) (float64, bool) {
	return resolveDailyLimit(limits, provider)
}

func resolveDailyLimit[T ~int64 | ~float64](limits map[string]T, provider string) (T, bool) {
	if len(limits) == 0 {
		var zero T
		return zero, false
	}
	key := strings.ToLower(provider)
	v, ok := limits[key]
	if !ok {
		v, ok = limits["*"]
	}
	return v, ok && v > 0
}

func scoreCandidates(configs map[string]Config, healthy []Backend, routing RoutingConfig, metrics map[string]ProviderRuntimeMetrics, intent RoutingIntent, estimatedTokens int) []CandidateScore {
	var allowed map[string]struct{}
	if len(routing.AllowedProviders) > 0 {
		allowed = make(map[string]struct{}, len(routing.AllowedProviders))
		for _, name := range routing.AllowedProviders {
			allowed[strings.ToLower(name)] = struct{}{}
		}
	}
	var denied map[string]struct{}
	if len(routing.DeniedProviders) > 0 {
		denied = make(map[string]struct{}, len(routing.DeniedProviders))
		for _, name := range routing.DeniedProviders {
			denied[strings.ToLower(name)] = struct{}{}
		}
	}

	mode := NormalizeRoutingMode(routing.Mode)
	scored := make([]CandidateScore, 0, len(healthy))
	for _, b := range healthy {
		cfg, ok := configs[b.Name]
		if !ok {
			continue
		}

		lname := strings.ToLower(b.Name)
		lbase := strings.ToLower(baseProviderName(b.Name))
		if len(allowed) > 0 {
			if _, ok := allowed[lname]; !ok {
				if _, ok := allowed[lbase]; !ok {
					continue
				}
			}
		}
		if _, blocked := denied[lname]; blocked {
			continue
		}
		if _, blocked := denied[lbase]; blocked {
			continue
		}

		// Context-window precheck: drop a candidate whose KNOWN context window
		// can't structurally hold the estimated prompt plus a completion reserve.
		// Unknown limits (0) always pass (graceful degradation).
		if !contextFits(cfg.ContextLength, estimatedTokens, contextHeadroomTokens) {
			continue
		}

		cost := estimateCostUSD(cfg, estimatedTokens)
		if routing.MaxCostUSD > 0 && cost > routing.MaxCostUSD {
			continue
		}

		m := metrics[b.Name]
		if exceedsDailyQuota(routing, b.Name, m, estimatedTokens, cost) {
			continue
		}

		latencyMs := float64(cfg.ExpectedLatencyMs)
		if latencyMs <= 0 && m.Requests > 0 {
			latencyMs = float64(m.TotalLatencyMs) / float64(m.Requests)
		}
		if latencyMs <= 0 {
			latencyMs = 1200
		}

		if routing.MaxLatencyMs > 0 && int(latencyMs) > routing.MaxLatencyMs {
			continue
		}

		errorRate := 0.0
		if m.Requests > 0 {
			errorRate = float64(m.Failures) / float64(m.Requests)
		}

		fit := capabilityFit(cfg.Capabilities, intent)
		runtimeQuality := runtimeQualityScore(m)
		score, reason := applyModeScoreNormalized(mode, cfg.Quality, runtimeQuality, fit, latencyMs, cost, errorRate)
		scored = append(scored, CandidateScore{
			Provider: b.Name,
			Model:    b.Model,
			Score:    score,
			Reason:   reason,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Provider < scored[j].Provider
		}
		return scored[i].Score > scored[j].Score
	})

	return scored
}

func baseProviderName(backendName string) string {
	if before, _, found := strings.Cut(backendName, ":"); found {
		return before
	}
	return backendName
}

func exceedsDailyQuota(routing RoutingConfig, provider string, m ProviderRuntimeMetrics, estimatedTokens int, estimatedCost float64) bool {
	today := time.Now().UTC().Format(dailyWindowFormat)
	if m.DailyWindow != today {
		m.DailyRequests = 0
		m.DailyTokens = 0
		m.DailyCostUSD = 0
	}

	if limit, ok := ResolveDailyLimitInt64(routing.DailyMaxRequests, provider); ok {
		if m.DailyRequests >= limit {
			return true
		}
	}
	if limit, ok := ResolveDailyLimitInt64(routing.DailyMaxTokens, provider); ok {
		if m.DailyTokens+int64(estimatedTokens) > limit {
			return true
		}
	}
	if limit, ok := ResolveDailyLimitFloat64(routing.DailyMaxCostUSD, provider); ok {
		if m.DailyCostUSD+estimatedCost > limit {
			return true
		}
	}

	return false
}

func capabilityFit(capabilities []string, intent RoutingIntent) float64 {
	if len(capabilities) == 0 {
		return 0.5
	}
	needle := strings.ToLower(string(intent))
	for _, c := range capabilities {
		if strings.ToLower(c) == needle {
			return 1.0
		}
	}
	return 0.3
}

// runtimeQualityScore returns the rolling quality score for a provider.
// Returns 0.5 (neutral) when no quality data has been recorded.
func runtimeQualityScore(m ProviderRuntimeMetrics) float64 {
	if m.QualityCount == 0 {
		return 0.5
	}
	return m.QualityScore
}

func applyModeScoreNormalized(mode string, quality, runtimeQuality, fit, latencyMs, costUSD, errorRate float64) (float64, string) {
	latencyScore := 1.0 / (1.0 + latencyMs/1000.0)
	costScore := 1.0 / (1.0 + costUSD*1000.0)
	reliability := 1.0 - errorRate
	blendedQuality := quality*0.6 + runtimeQuality*0.4
	reason := func(policy string) string {
		return fmt.Sprintf("%s: quality=%.2f runtime_quality=%.2f fit=%.2f latency=%.0fms estimated_cost=$%.6f reliability=%.2f",
			policy, quality, runtimeQuality, fit, latencyMs, costUSD, reliability)
	}

	switch mode {
	case string(RoutingModeFastest):
		return 0.65*latencyScore + 0.15*reliability + 0.15*fit + 0.05*blendedQuality, reason("lowest latency weighted")
	case string(RoutingModeCheapest):
		return 0.65*costScore + 0.15*reliability + 0.15*fit + 0.05*blendedQuality, reason("lowest cost weighted")
	case string(RoutingModeQuality):
		return 0.55*blendedQuality + 0.25*fit + 0.20*reliability, reason("highest quality weighted")
	default:
		return 0.30*blendedQuality + 0.20*fit + 0.20*latencyScore + 0.15*costScore + 0.15*reliability, reason("balanced policy")
	}
}
