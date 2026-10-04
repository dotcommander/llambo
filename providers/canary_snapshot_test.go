package providers

import (
	"context"
	"errors"
	whtypes "github.com/garyblankenship/wormhole/v3/types"
	"path/filepath"
	"sync"
	"testing"
)

func canaryTestOwner(t *testing.T, count int) (*OpenAIClients, *RouteEventLogger) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	events := NewRouteEventLogger(path)
	t.Cleanup(events.Close)
	configs := map[string]Config{"baseline": {Enabled: true, Priority: 7, Model: "Base"}, "canary": {Enabled: true, Priority: 19, Model: "Canary", Models: []string{"Other"}, ExtraBody: map[string]any{"nested": map[string]any{"value": "original"}}}}
	oc := &OpenAIClients{configs: configs, RoutingConfig: RoutingConfig{Mode: "balanced", EventsPath: path, Canary: &CanaryConfig{Provider: "canary", Baseline: "baseline", TrafficPct: 0.5, PromoteAfter: 5}}}
	for range count {
		appendCanaryEvidence(t, events)
	}
	oc.routingSnapshot(configs)
	return oc, events
}

func appendCanaryEvidence(t *testing.T, events *RouteEventLogger) {
	t.Helper()
	for _, event := range []RouteEvent{{ChosenProvider: "canary", IsCanary: true, Success: true, LatencyMs: 50}, {ChosenProvider: "baseline", Success: true, LatencyMs: 100}} {
		if err := events.Log(event); err != nil {
			t.Fatal(err)
		}
	}
}

func rawCanaryConfig(oc *OpenAIClients) *GlobalConfig {
	configs, routing := oc.routingSnapshot(nil)
	return &GlobalConfig{Providers: cloneProviderConfigs(configs), Routing: cloneRoutingConfig(routing)}
}

func TestCanarySnapshotPublicationPreservesOldGeneration(t *testing.T) {
	t.Parallel()
	oc, _ := canaryTestOwner(t, 5)
	oldConfigs, oldRouting := oc.routingSnapshot(nil)
	client := &closeTrackingProvider{}
	generation := &clientGeneration{client: client, leases: 1, key: "retained-key"}
	limiter := make(chan struct{}, 2)
	limiter <- struct{}{}
	oc.Clients = map[string]whtypes.Provider{"canary": client}
	oc.generations = map[string]*clientGeneration{"canary": generation}
	oc.limiters = map[string]chan struct{}{"canary": limiter}
	oc.Backends = []Backend{{Name: "canary", Model: "Canary"}}
	breaker := NewCircuitBreaker([]string{"canary"}, nil)
	oc.CircuitBreaker = breaker
	keys := NewKeyRotator(oc.configs)
	oc.KeyRotator = keys
	cfg := rawCanaryConfig(oc)
	oc.canaryUpdate = func(ctx context.Context, mutate func(*GlobalConfig) error) error { return mutate(cfg) }
	original := oc.configs["canary"]
	original.Models[0] = "mutated"
	original.ExtraBody["nested"].(map[string]any)["value"] = "mutated"
	oldOwnerConfigs := oc.configs
	if err := oc.evaluateAndPublishCanary(context.Background(), &routingSnapshot{configs: oldConfigs, routing: oldRouting}); err != nil {
		t.Fatal(err)
	}
	newConfigs, newRouting := oc.routingSnapshot(nil)
	if oldRouting.Canary == nil || oldConfigs["canary"].Priority != 19 {
		t.Fatal("in-flight generation changed")
	}
	if newRouting.Canary != nil || newConfigs["canary"].Priority != 7 {
		t.Fatal("new generation not promoted")
	}
	if oldConfigs["canary"].Models[0] != "Other" || oldConfigs["canary"].ExtraBody["nested"].(map[string]any)["value"] != "original" {
		t.Fatal("snapshot aliases caller config")
	}
	if oc.configs["canary"].Priority != oldOwnerConfigs["canary"].Priority {
		t.Fatal("resource owner config changed")
	}
	if oc.Clients["canary"] != client || oc.generations["canary"] != generation || generation.leases != 1 || client.closes.Load() != 0 || oc.limiters["canary"] != limiter || len(limiter) != 1 || oc.CircuitBreaker != breaker || oc.KeyRotator != keys || oc.Backends[0].Model != "Canary" {
		t.Fatal("promotion replaced or altered provider resources")
	}
}

func TestCanaryEvaluatorRetriesInsufficientEvidence(t *testing.T) {
	t.Parallel()
	oc, events := canaryTestOwner(t, 4)
	var calls int
	cfg := rawCanaryConfig(oc)
	oc.canaryUpdate = func(ctx context.Context, mutate func(*GlobalConfig) error) error { calls++; return mutate(cfg) }
	configs, routing := oc.routingSnapshot(nil)
	snapshot := &routingSnapshot{configs: configs, routing: routing}
	oc.scheduleCanaryEvaluation(context.Background(), snapshot)
	waitCanaryEvaluation(oc)
	if calls != 0 {
		t.Fatal("promoted below threshold")
	}
	appendCanaryEvidence(t, events)
	oc.scheduleCanaryEvaluation(context.Background(), snapshot)
	waitCanaryEvaluation(oc)
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	_, r := oc.routingSnapshot(nil)
	if r.Canary != nil {
		t.Fatal("later completion did not promote")
	}
	oc.stopCanaryEvaluator()
}

func waitCanaryEvaluation(oc *OpenAIClients) {
	oc.canaryMu.Lock()
	done := oc.canaryEvaluator.done
	oc.canaryMu.Unlock()
	<-done
}

func TestCanaryEvaluatorCoalescesCompletionDuringPersistenceFailure(t *testing.T) {
	t.Parallel()
	oc, _ := canaryTestOwner(t, 5)
	started, release := make(chan struct{}), make(chan struct{})
	var calls int
	cfg := rawCanaryConfig(oc)
	oc.canaryUpdate = func(ctx context.Context, mutate func(*GlobalConfig) error) error {
		calls++
		if calls == 1 {
			close(started)
			<-release
			return errors.New("publication failed")
		}
		return mutate(cfg)
	}
	configs, routing := oc.routingSnapshot(nil)
	snapshot := &routingSnapshot{configs: configs, routing: routing}
	oc.scheduleCanaryEvaluation(context.Background(), snapshot)
	<-started
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); oc.scheduleCanaryEvaluation(context.Background(), snapshot) }()
	}
	wg.Wait()
	close(release)
	waitCanaryEvaluation(oc)
	if calls != 2 {
		t.Fatalf("calls = %d, want coalesced retry", calls)
	}
	_, r := oc.routingSnapshot(nil)
	if r.Canary != nil {
		t.Fatal("retry did not publish live state")
	}
	oc.stopCanaryEvaluator()
}

func TestCanaryRejectsStaleDiskIdentity(t *testing.T) {
	t.Parallel()
	oc, _ := canaryTestOwner(t, 5)
	cfg := rawCanaryConfig(oc)
	cfg.Routing.Canary.StartedAt = "different-generation"
	oc.canaryUpdate = func(ctx context.Context, mutate func(*GlobalConfig) error) error { return mutate(cfg) }
	configs, routing := oc.routingSnapshot(nil)
	err := oc.evaluateAndPublishCanary(context.Background(), &routingSnapshot{configs: configs, routing: routing})
	if !errors.Is(err, errStaleCanary) {
		t.Fatalf("error = %v", err)
	}
	if cfg.Routing.Canary == nil || cfg.Providers["canary"].Priority != 19 {
		t.Fatal("stale persisted config mutated")
	}
	_, r := oc.routingSnapshot(nil)
	if r.Canary == nil {
		t.Fatal("stale evaluation changed live state")
	}
}

func TestCanaryCleanupCancelsAndJoinsEvaluator(t *testing.T) {
	t.Parallel()
	oc, _ := canaryTestOwner(t, 5)
	started, exited := make(chan struct{}), make(chan struct{})
	oc.canaryUpdate = func(ctx context.Context, mutate func(*GlobalConfig) error) error {
		close(started)
		<-ctx.Done()
		close(exited)
		return ctx.Err()
	}
	configs, routing := oc.routingSnapshot(nil)
	snapshot := &routingSnapshot{configs: configs, routing: routing}
	oc.scheduleCanaryEvaluation(context.Background(), snapshot)
	<-started
	oc.stopCanaryEvaluator()
	select {
	case <-exited:
	default:
		t.Fatal("cleanup did not join")
	}
	oc.scheduleCanaryEvaluation(context.Background(), snapshot)
	oc.canaryMu.Lock()
	running := oc.canaryEvaluator.running
	oc.canaryMu.Unlock()
	if running {
		t.Fatal("evaluator restarted after cleanup")
	}
}

func TestCanaryEvidenceExcludesIncompleteStreams(t *testing.T) {
	t.Parallel()
	canary := &CanaryConfig{Provider: "canary", Baseline: "baseline", PromoteAfter: 5}
	events := make([]RouteEvent, 0)
	for range 5 {
		events = append(events, RouteEvent{ChosenProvider: "canary", IsCanary: true, Success: true, LatencyMs: 50}, RouteEvent{ChosenProvider: "baseline", Success: true, LatencyMs: 100})
	}
	for range 20 {
		events = append(events, RouteEvent{ChosenProvider: "canary", IsCanary: true, PromotionIneligible: true, Success: false, LatencyMs: 10000})
	}
	status := EvaluateCanary(canary, events, nil)
	if status.CanaryRequests != 5 || !status.ShouldPromote {
		t.Fatalf("ineligible stream evidence counted: %+v", status)
	}
	// Ordinary completed non-stream failures retain the existing scoring policy.
	events = append(events, RouteEvent{ChosenProvider: "canary", IsCanary: true, Success: false})
	status = EvaluateCanary(canary, events, nil)
	if status.CanaryRequests != 6 || status.ShouldPromote {
		t.Fatalf("ordinary failure excluded: %+v", status)
	}
}

func TestCanaryPromotionKeepsFullSnapshotForExactRequest(t *testing.T) {
	t.Parallel()
	oc, _ := canaryTestOwner(t, 5)
	cfg := rawCanaryConfig(oc)
	oc.canaryUpdate = func(ctx context.Context, mutate func(*GlobalConfig) error) error { return mutate(cfg) }
	var counter uint64
	coordinator := newExecutionCoordinator(oc, nil, &counter)
	// Exact request selection narrows the request's configs and replaces its
	// model while preserving the original generation for live publication.
	narrowed := coordinator.configs["canary"]
	narrowed.Model = "Exact/RequestedModel"
	coordinator.configs = map[string]Config{"canary": narrowed}
	coordinator.checkCanaryAutoPromote(context.Background(), chatExecutionPlan{isCanary: true})
	waitCanaryEvaluation(oc)
	published, routing := oc.routingSnapshot(nil)
	if len(published) != 2 || published["canary"].Model != "Canary" || published["baseline"].Model != "Base" || routing.Canary != nil {
		t.Fatalf("published request-scoped configs: %+v", published)
	}
	oc.stopCanaryEvaluator()
}
