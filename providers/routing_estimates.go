package providers

func estimateCostUSD(cfg Config, totalTokens int) float64 {
	if totalTokens <= 0 {
		return 0
	}
	if cfg.InputCostPM <= 0 && cfg.OutputCostPM <= 0 {
		return 0
	}
	// lightweight estimate: assume 60/40 in/out split
	in := float64(totalTokens) * 0.60
	out := float64(totalTokens) * 0.40
	return (in/1_000_000.0)*cfg.InputCostPM + (out/1_000_000.0)*cfg.OutputCostPM
}

// costFromTokenSplit computes cost from known prompt/completion token counts.
// Returns 0 when the config carries no pricing.
func costFromTokenSplit(cfg Config, promptTokens, completionTokens int) float64 {
	if cfg.InputCostPM <= 0 && cfg.OutputCostPM <= 0 {
		return 0
	}
	in := float64(promptTokens) / 1_000_000.0 * cfg.InputCostPM
	out := float64(completionTokens) / 1_000_000.0 * cfg.OutputCostPM
	return in + out
}

// SimulateRoute computes a routing decision for a given intent and token estimate.
func SimulateRoute(configs map[string]Config, routing RoutingConfig, metrics map[string]ProviderRuntimeMetrics, intent RoutingIntent, estimatedTokens int) RouteDecision {
	entries := FilterEnabledProviders(configs)
	healthy := make([]Backend, 0, len(entries))
	for _, e := range entries {
		healthy = append(healthy, Backend{Name: e.Name, Model: e.Config.Model})
	}

	candidates := scoreCandidates(configs, healthy, routing, metrics, intent, estimatedTokens)
	decision := RouteDecision{
		Mode:       routing.Mode,
		Intent:     intent,
		Candidates: candidates,
	}
	if len(candidates) > 0 {
		decision.Chosen = candidates[0].Provider
		decision.Reason = candidates[0].Reason
	}
	return decision
}

// EstimateProviderLatencyMs estimates latency using runtime metrics first, then config hint.
func EstimateProviderLatencyMs(provider string, cfg Config, metrics map[string]ProviderRuntimeMetrics) int64 {
	if m, ok := metrics[provider]; ok && m.Requests > 0 {
		return m.TotalLatencyMs / m.Requests
	}
	if cfg.ExpectedLatencyMs > 0 {
		return int64(cfg.ExpectedLatencyMs)
	}
	return 1200
}

// EstimateProviderCostUSD estimates cost using provider config and total tokens.
func EstimateProviderCostUSD(cfg Config, totalTokens int) float64 {
	return estimateCostUSD(cfg, totalTokens)
}
