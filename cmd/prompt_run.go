package cmd

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// promptRun owns the routing metrics store for one CLI invocation. Its child
// providers share that store, but never save it themselves.
type promptRun struct {
	metrics           *providers.RoutingMetricsStore
	errOut            io.Writer
	providerLimiters  map[string]chan struct{}
	providerLimitersM sync.Mutex
}

func newPromptRun(errOut io.Writer) (*promptRun, error) {
	routing := providers.RoutingConfig{}
	routing.ApplyDefaults()
	metrics, err := providers.NewRoutingMetricsStore(routing.MetricsPath)
	if err != nil {
		return nil, fmt.Errorf("init routing metrics store: %w", err)
	}
	return &promptRun{metrics: metrics, errOut: errOut, providerLimiters: make(map[string]chan struct{})}, nil
}

func (r *promptRun) close() {
	if r == nil || r.metrics == nil {
		return
	}
	if err := r.metrics.Save(); err != nil {
		fmt.Fprintf(r.errOut, "Warning: save routing metrics: %v\n", err)
	}
}

func (r *promptRun) execute(ctx context.Context, entry providers.ProviderEntry, promptText, systemPrompt string, timeoutSecs int) PromptResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	release, err := r.acquireProvider(ctx, entry)
	if err != nil {
		return PromptResult{Provider: entry.Name, Model: entry.Config.Model, Error: err, Latency: time.Since(start)}
	}
	defer release()

	provider, err := providers.NewOpenAIWithSharedRoutingMetrics(map[string]providers.Config{entry.Name: entry.Config}, r.metrics)
	if err != nil {
		return PromptResult{Provider: entry.Name, Model: entry.Config.Model, Error: fmt.Errorf("failed to create provider: %w", err), Latency: time.Since(start)}
	}
	defer provider.Shutdown()

	result, err := provider.ChatWithInfoContext(ctx, systemPrompt, promptText)
	if err != nil {
		return PromptResult{Provider: entry.Name, Model: entry.Config.Model, Error: err, Latency: time.Since(start)}
	}
	return PromptResult{Provider: entry.Name, Model: result.Model, Response: result.Content, Latency: time.Since(start)}
}

// acquireProvider limits selected-model fanout to the configured capacity of
// each provider. promptRun creates one provider client per target, so this
// invocation-scoped limiter preserves the provider's shared Workers contract.
func (r *promptRun) acquireProvider(ctx context.Context, entry providers.ProviderEntry) (func(), error) {
	r.providerLimitersM.Lock()
	if r.providerLimiters == nil {
		r.providerLimiters = make(map[string]chan struct{})
	}
	limiter := r.providerLimiters[entry.Name]
	if limiter == nil {
		limiter = make(chan struct{}, entry.Config.GetWorkers())
		r.providerLimiters[entry.Name] = limiter
	}
	r.providerLimitersM.Unlock()

	select {
	case limiter <- struct{}{}:
		return func() { <-limiter }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
