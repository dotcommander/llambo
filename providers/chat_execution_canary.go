package providers

import "log/slog"

// checkCanaryAutoPromote evaluates canary criteria after a successful canary
// request. The disk-bound config load/save is dispatched to a single goroutine
// guarded by this instance's canaryOnce gate so concurrent canary requests
// cannot race on the config file rename, and so the LoadRawGlobalConfig/
// SaveGlobalConfig pair never executes on a request goroutine. The gate lives
// on c.oc (per-instance), so a fresh OpenAIClients can promote again.
func (c executionCoordinator) checkCanaryAutoPromote(plan chatExecutionPlan) {
	if !plan.isCanary || c.oc.RoutingConfig.Canary == nil || c.oc.RoutingConfig.Canary.PromoteAfter <= 0 {
		return
	}
	c.oc.canaryOnce().Do(func() {
		// Snapshot the data the background goroutine needs so we don't
		// race against the request goroutine's mutation of c.oc.
		canaryCfg := c.oc.RoutingConfig.Canary
		eventsPath := c.oc.RoutingConfig.EventsPath
		configs := c.configs
		go performCanaryAutoPromote(canaryCfg, eventsPath, configs)
	})
}

// performCanaryAutoPromote runs the (potentially slow) config read/evaluate/
// write off the request goroutine. Returning early — without promoting —
// leaves this instance's canaryOnce gate consumed; future requests on the same
// OpenAIClients no-op, matching the previous behavior where a missed window
// also wouldn't retry. A new instance starts with a fresh gate.
func performCanaryAutoPromote(canaryCfg *CanaryConfig, eventsPath string, configs map[string]Config) {
	events, err := ReadRouteEvents(eventsPath)
	if err != nil {
		return
	}
	status := EvaluateCanary(canaryCfg, events, configs)
	if status == nil || !status.ShouldPromote {
		return
	}

	cfg, err := LoadRawGlobalConfig()
	if err != nil {
		slog.Error("canary auto-promote: load config failed", "error", err)
		return
	}

	canaryName, targetPriority := PromoteCanaryConfig(cfg, *canaryCfg)

	if err := SaveGlobalConfig(cfg); err != nil {
		slog.Error("canary auto-promote: save config failed", "error", err)
		return
	}

	slog.Info("canary auto-promotion triggered",
		"canary", canaryName,
		"priority", targetPriority,
		"requests", status.CanaryRequests,
		"reason", status.PromoteReason,
	)
}
