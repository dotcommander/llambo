package gateway

import (
	"net/http"
	"time"

	"github.com/dotcommander/llambo/providers"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	backends := make(map[string]BackendHealthResponse)
	allHealthy := true
	cb := s.queue.GetCircuitBreaker()
	for _, name := range s.queue.BackendNames() {
		health := cb.GetHealth(name)
		isHealthy := cb.IsHealthy(name)
		if !isHealthy {
			allHealthy = false
		}
		bh := BackendHealthResponse{Healthy: isHealthy, Failures: health.Failures}
		if health.Disabled {
			bh.DisabledAt = &health.DisabledAt
		}
		backends[name] = bh
	}
	status := "healthy"
	if !allHealthy {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:        status,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		Backends:      backends,
	})
}

func (s *Server) handleProviders(w http.ResponseWriter, _ *http.Request) {
	var providerList []ProviderInfo
	cb := s.queue.GetCircuitBreaker()
	for _, entry := range providers.FilterEnabledProviders(s.configs) {
		providerList = append(providerList, ProviderInfo{
			Name: entry.Name, Model: entry.Config.Model, Enabled: entry.Config.Enabled,
			Healthy: cb.IsHealthy(entry.Name), Priority: entry.Config.Priority, Workers: entry.Config.GetWorkers(),
		})
	}
	writeJSON(w, http.StatusOK, ProvidersResponse{Providers: providerList})
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	oc := s.provider.GetOpenAIClients()
	costTracker := oc.CostTracker
	total := costTracker.GetTotal()
	writeJSON(w, http.StatusOK, StatsResponse{
		Providers: buildProviderStatsResponse(costTracker.GetStats()),
		Total: ProviderStatsResponse{
			Requests: total.Requests, PromptTokens: total.PromptTokens,
			CompletionTokens: total.CompletionTokens, TotalTokens: total.TotalTokens, TotalCostUSD: total.TotalCostUSD,
		},
		Routing: buildRoutingStatsResponse(oc),
	})
}

func buildProviderStatsResponse(stats map[string]providers.ProviderStats) map[string]ProviderStatsResponse {
	result := make(map[string]ProviderStatsResponse, len(stats))
	for name, stat := range stats {
		result[name] = ProviderStatsResponse{
			Requests: stat.Requests, PromptTokens: stat.PromptTokens,
			CompletionTokens: stat.CompletionTokens, TotalTokens: stat.TotalTokens, TotalCostUSD: stat.TotalCostUSD,
		}
	}
	return result
}

func buildRoutingStatsResponse(oc *providers.OpenAIClients) map[string]RoutingStatsResponse {
	if oc == nil || oc.RoutingMetrics == nil {
		return map[string]RoutingStatsResponse{}
	}
	snapshot := oc.RoutingMetrics.Snapshot()
	result := make(map[string]RoutingStatsResponse, len(snapshot))
	for provider, stat := range snapshot {
		avg := int64(0)
		if stat.Requests > 0 {
			avg = stat.TotalLatencyMs / stat.Requests
		}
		lastUpdated := ""
		if !stat.LastUpdated.IsZero() {
			lastUpdated = stat.LastUpdated.Format(time.RFC3339)
		}
		limitReq, _ := providers.ResolveDailyLimitInt64(oc.RoutingConfig.DailyMaxRequests, provider)
		limitTok, _ := providers.ResolveDailyLimitInt64(oc.RoutingConfig.DailyMaxTokens, provider)
		limitCost, _ := providers.ResolveDailyLimitFloat64(oc.RoutingConfig.DailyMaxCostUSD, provider)
		result[provider] = RoutingStatsResponse{
			Requests: stat.Requests, Successes: stat.Successes, Failures: stat.Failures, Timeouts: stat.Timeouts,
			AvgLatencyMs: avg, TotalCostUSD: stat.TotalCostUSD, DailyWindow: stat.DailyWindow,
			DailyRequests: stat.DailyRequests, DailyTokens: stat.DailyTokens, DailyCostUSD: stat.DailyCostUSD,
			DailyLimitReq: limitReq, DailyLimitTok: limitTok, DailyLimitCost: limitCost,
			DailyExhausted: (limitReq > 0 && stat.DailyRequests >= limitReq) ||
				(limitTok > 0 && stat.DailyTokens >= limitTok) || (limitCost > 0 && stat.DailyCostUSD >= limitCost),
			LastUpdatedISO: lastUpdated,
		}
	}
	return result
}
