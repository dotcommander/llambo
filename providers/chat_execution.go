package providers

import (
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"
)

type chatExecutionPlan struct {
	selected        providerInfo
	enabled         []providerInfo
	decision        *RouteDecision
	plannedProvider string
	intent          RoutingIntent
	estimatedTokens int
	candidates      []CandidateScore
	isCanary        bool
}

type chatExecutionOutcome struct {
	provider providerInfo
	result   chatRequestResult
	decision *RouteDecision
}

type executionCoordinator struct {
	oc                  *OpenAIClients
	configs             map[string]Config
	snapshotConfigs     map[string]Config
	counter             *uint64
	routing             RoutingConfig
	promotionIneligible bool
	failoverCallback    FailoverCallback
}

func newExecutionCoordinator(oc *OpenAIClients, configs map[string]Config, counter *uint64) executionCoordinator {
	configs, routing := oc.routingSnapshot(configs)
	return executionCoordinator{
		oc:              oc,
		configs:         configs,
		snapshotConfigs: configs,
		counter:         counter,
		routing:         routing,
	}
}

func (c executionCoordinator) collectEnabledProviders() ([]providerInfo, int) {
	enabled, totalWeight, _, _ := c.collectEnabledProvidersForPrompt("", "")
	return enabled, totalWeight
}

func (c executionCoordinator) collectEnabledProvidersForPrompt(systemPrompt, userContent string) ([]providerInfo, int, *RouteDecision, int) {
	healthy := c.oc.GetHealthyBackends()
	intent := DetectIntent(systemPrompt, userContent)
	estimatedTokens := EstimatePromptTokens(systemPrompt, userContent)
	metrics := map[string]ProviderRuntimeMetrics{}
	if c.oc.RoutingMetrics != nil {
		metrics = c.oc.RoutingMetrics.Snapshot()
	}
	scored := scoreCandidates(c.configs, healthy, c.routing, metrics, intent, estimatedTokens)
	decision := &RouteDecision{Mode: c.routing.Mode, Intent: intent, Candidates: scored}

	if len(scored) > 0 {
		decision.Chosen = scored[0].Provider
		decision.Reason = scored[0].Reason
	}

	var enabled []providerInfo
	totalWeight := 0
	if len(scored) == 0 {
		for _, b := range healthy {
			cfg, exists := c.configs[b.Name]
			if !exists {
				continue
			}
			weight := cfg.GetWorkers()
			enabled = append(enabled, providerInfo{name: b.Name, cfg: cfg, weight: weight})
			totalWeight += weight
		}
		return enabled, totalWeight, decision, estimatedTokens
	}

	for _, candidate := range scored {
		cfg := c.configs[candidate.Provider]
		weight := cfg.GetWorkers()
		enabled = append(enabled, providerInfo{name: candidate.Provider, cfg: cfg, weight: weight})
		totalWeight += weight
	}
	return enabled, totalWeight, decision, estimatedTokens
}

func (c executionCoordinator) collectConfiguredProviders() ([]providerInfo, int) {
	entries := FilterEnabledProviders(c.configs)
	enabled := make([]providerInfo, 0, len(entries))
	totalWeight := 0
	for _, entry := range entries {
		weight := entry.Config.GetWorkers()
		enabled = append(enabled, providerInfo{name: entry.Name, cfg: entry.Config, weight: weight})
		totalWeight += weight
	}
	return enabled, totalWeight
}

func (c executionCoordinator) selectWeightedProvider(enabled []providerInfo, totalWeight int) providerInfo {
	if len(enabled) == 0 {
		return providerInfo{}
	}
	if totalWeight <= 0 {
		idx := atomic.AddUint64(c.counter, 1) - 1
		return enabled[idx%uint64(len(enabled))]
	}

	counter := atomic.AddUint64(c.counter, 1)
	slot := int(counter % uint64(totalWeight))

	cumulative := 0
	for _, info := range enabled {
		cumulative += info.weight
		if slot < cumulative {
			return info
		}
	}
	return enabled[0]
}

func (c executionCoordinator) selectProvider(enabled []providerInfo, totalWeight int, decision *RouteDecision) providerInfo {
	if decision == nil || len(decision.Candidates) == 0 {
		return c.selectWeightedProvider(enabled, totalWeight)
	}

	maxScore := decision.Candidates[0].Score
	var top []string
	for _, candidate := range decision.Candidates {
		if math.Abs(candidate.Score-maxScore) > 1e-9 {
			break
		}
		top = append(top, candidate.Provider)
	}

	chosen := top[0]
	if len(top) > 1 {
		idx := atomic.AddUint64(c.counter, 1) - 1
		chosen = top[idx%uint64(len(top))]
	}
	for _, info := range enabled {
		if info.name == chosen {
			return info
		}
	}
	return c.selectWeightedProvider(enabled, totalWeight)
}

func (c executionCoordinator) plan(systemPrompt, userContent string, allowUnhealthyFallback bool) (chatExecutionPlan, error) {
	enabled, totalWeight, decision, estimatedTokens := c.collectEnabledProvidersForPrompt(systemPrompt, userContent)
	if len(enabled) == 0 && allowUnhealthyFallback {
		enabled, totalWeight = c.collectConfiguredProviders()
		if decision == nil {
			decision = &RouteDecision{Mode: c.routing.Mode}
		}
	}
	if len(enabled) == 0 {
		return chatExecutionPlan{}, fmt.Errorf("no enabled providers available")
	}

	// Canary routing: deterministically route a fraction of requests to the canary provider
	isCanary := false
	canary := c.routing.Canary
	var selected providerInfo
	if canary != nil && shouldRouteToCanary(canary, systemPrompt, userContent) {
		for _, info := range enabled {
			if info.name == canary.Provider {
				selected = info
				isCanary = true
				break
			}
		}
		if !isCanary {
			slog.Warn("canary provider not available, falling back to normal routing", "canary", canary.Provider, "reason", "not in healthy backends")
		}
	}
	if !isCanary {
		selected = c.selectProvider(enabled, totalWeight, decision)
	}
	if decision != nil {
		decision.Chosen = selected.name
		if reason, ok := decisionReasonForProvider(decision, selected.name); ok {
			decision.Reason = reason
		}
	}

	return chatExecutionPlan{
		selected:        selected,
		enabled:         enabled,
		decision:        decision,
		plannedProvider: selected.name,
		intent:          decision.Intent,
		estimatedTokens: estimatedTokens,
		candidates:      decisionCandidates(decision),
		isCanary:        isCanary,
	}, nil
}
