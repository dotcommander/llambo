package providers

import (
	"sync"
)

// ProviderStats holds aggregated statistics for a single provider
type ProviderStats struct {
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
}

// CostTracker tracks token usage and costs per provider
type CostTracker struct {
	stats map[string]*ProviderStats
	mu    sync.RWMutex
}

// NewCostTracker creates a new cost tracker for the given backends
func NewCostTracker(backendNames []string) *CostTracker {
	stats := make(map[string]*ProviderStats)
	for _, name := range backendNames {
		stats[name] = &ProviderStats{}
	}
	return &CostTracker{stats: stats}
}

// Record records usage from a successful request
func (ct *CostTracker) Record(backend string, usage *LLMUsage) {
	if usage == nil {
		return
	}

	ct.mu.Lock()
	defer ct.mu.Unlock()

	stats, ok := ct.stats[backend]
	if !ok {
		// Backend not pre-registered, create entry
		stats = &ProviderStats{}
		ct.stats[backend] = stats
	}

	stats.Requests++
	stats.PromptTokens += int64(usage.PromptTokens)
	stats.CompletionTokens += int64(usage.CompletionTokens)
	stats.TotalTokens += int64(usage.TotalTokens)

	if usage.Cost != nil {
		stats.TotalCostUSD += usage.Cost.TotalCost
	}
}

// GetStats returns a copy of stats for all providers
func (ct *CostTracker) GetStats() map[string]ProviderStats {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	result := make(map[string]ProviderStats)
	for name, stats := range ct.stats {
		result[name] = *stats
	}
	return result
}

// GetTotal returns aggregated stats across all providers
func (ct *CostTracker) GetTotal() ProviderStats {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	var total ProviderStats
	for _, stats := range ct.stats {
		total.Requests += stats.Requests
		total.PromptTokens += stats.PromptTokens
		total.CompletionTokens += stats.CompletionTokens
		total.TotalTokens += stats.TotalTokens
		total.TotalCostUSD += stats.TotalCostUSD
	}
	return total
}

// Reset clears all stats (useful for testing)
func (ct *CostTracker) Reset() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	for _, stats := range ct.stats {
		*stats = ProviderStats{}
	}
}
