package providers

import (
	"context"
	"errors"
)

var errStaleCanary = errors.New("canary identity changed")

// canaryEvaluator coalesces notifications, including completions received while
// evaluation or publication is running. An unsuccessful evaluation consumes only
// its notification, never the owner's eligibility for another completion.
type canaryEvaluator struct {
	pending *routingSnapshot
	running bool
	done    chan struct{}
	cancel  context.CancelFunc
}

func (c executionCoordinator) checkCanaryAutoPromote(ctx context.Context, plan chatExecutionPlan) {
	if ctx.Err() != nil || c.routing.Canary == nil || c.routing.Canary.PromoteAfter <= 0 {
		return
	}
	configs := c.snapshotConfigs
	if configs == nil {
		configs = c.configs
	}
	c.oc.scheduleCanaryEvaluation(ctx, &routingSnapshot{configs: configs, routing: c.routing})
}

func (oc *OpenAIClients) scheduleCanaryEvaluation(ctx context.Context, snapshot *routingSnapshot) {
	oc.canaryMu.Lock()
	defer oc.canaryMu.Unlock()
	if oc.canaryStopped {
		return
	}
	if oc.canaryEvaluator == nil {
		oc.canaryEvaluator = &canaryEvaluator{}
	}
	e := oc.canaryEvaluator
	e.pending = snapshot
	if e.running {
		return
	}
	e.running = true
	e.done = make(chan struct{})
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	e.cancel = cancel
	// Exits when the pending notifications are drained or cleanup cancels it.
	go oc.runCanaryEvaluator(runCtx, e)
}

func (oc *OpenAIClients) runCanaryEvaluator(ctx context.Context, e *canaryEvaluator) {
	defer e.cancel()
	for {
		oc.canaryMu.Lock()
		snapshot := e.pending
		e.pending = nil
		if snapshot == nil || oc.canaryStopped || ctx.Err() != nil {
			e.running = false
			close(e.done)
			oc.canaryMu.Unlock()
			return
		}
		oc.canaryMu.Unlock()
		if err := oc.evaluateAndPublishCanary(ctx, snapshot); err != nil && !errors.Is(err, errStaleCanary) && !errors.Is(err, context.Canceled) {
			oc.loggerOrDefault().Warn("canary auto-promotion failed", "error", err)
		}
	}
}

func (oc *OpenAIClients) stopCanaryEvaluator() {
	oc.canaryMu.Lock()
	oc.canaryStopped = true
	e := oc.canaryEvaluator
	var done chan struct{}
	if e != nil && e.running {
		e.cancel()
		done = e.done
	}
	oc.canaryMu.Unlock()
	if done != nil {
		<-done
	}
}

func (oc *OpenAIClients) evaluateAndPublishCanary(ctx context.Context, snapshot *routingSnapshot) error {
	canary := snapshot.routing.Canary
	if canary == nil || canary.PromoteAfter <= 0 {
		return nil
	}
	events, err := ReadRouteEvents(snapshot.routing.EventsPath)
	if err != nil {
		return err
	}
	status := EvaluateCanary(canary, events, snapshot.configs)
	if status == nil || !status.ShouldPromote {
		return nil
	}
	// Prepare the runtime generation before publication; raw persisted values
	// supply only the promotion priority, never credentials or unrelated edits.
	var prepared *routingSnapshot
	update := oc.canaryUpdate
	if update == nil {
		update = UpdateGlobalConfig
	}
	err = update(ctx, func(cfg *GlobalConfig) error {
		if cfg.Routing.Canary == nil || *cfg.Routing.Canary != *canary {
			return errStaleCanary
		}
		provider, exists := cfg.Providers[canary.Provider]
		if !exists || !provider.Enabled {
			return errStaleCanary
		}
		_, priority := PromoteCanaryConfig(cfg, *canary)
		prepared = &routingSnapshot{configs: cloneProviderConfigs(snapshot.configs), routing: cloneRoutingConfig(snapshot.routing)}
		runtimeProvider, exists := prepared.configs[canary.Provider]
		if !exists {
			return errStaleCanary
		}
		runtimeProvider.Priority = priority
		prepared.configs[canary.Provider] = runtimeProvider
		prepared.routing.Canary = nil
		return nil
	})
	if err != nil {
		return err
	}
	oc.routingMu.Lock()
	defer oc.routingMu.Unlock()
	// A delayed evaluation may never overwrite a newer published generation.
	if oc.routingState != nil && (oc.routingState.routing.Canary == nil || *oc.routingState.routing.Canary != *canary) {
		return errStaleCanary
	}
	oc.routingState = prepared
	oc.loggerOrDefault().Info("canary auto-promotion triggered", "canary", canary.Provider, "requests", status.CanaryRequests, "reason", status.PromoteReason)
	return nil
}
