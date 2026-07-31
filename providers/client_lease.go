package providers

import (
	"context"
	"errors"
	"sync"

	whtypes "github.com/garyblankenship/wormhole/v3/types"
)

var (
	// ErrBackendUnavailable is returned when a backend becomes unhealthy while
	// an attempt waits for its admission slot. The attempt did not reach the
	// provider and must not affect breaker health.
	ErrBackendUnavailable = errors.New("backend unavailable after admission")
	ErrClientsClosed      = errors.New("provider clients are closed")
)

type clientGeneration struct {
	client  whtypes.Provider
	key     string
	leases  int
	retired bool
	closed  bool
}

// clientLease keeps one immutable provider generation alive for a complete
// request attempt. It deliberately exposes neither the configured key nor any
// mutable client map to callers.
type clientLease struct {
	oc      *OpenAIClients
	backend string
	gen     *clientGeneration
	once    sync.Once
}

func (l *clientLease) Client() whtypes.Provider { return l.gen.client }

func (l *clientLease) keyForRotation() string { return l.gen.key }

func (l *clientLease) generationForRotation() *clientGeneration { return l.gen }

func (l *clientLease) Release() {
	if l == nil || l.oc == nil || l.gen == nil {
		return
	}
	l.once.Do(func() {
		l.oc.mu.Lock()
		defer l.oc.mu.Unlock()
		l.gen.leases--
		l.oc.closeRetiredGenerationLocked(l.gen)
	})
}

func (oc *OpenAIClients) acquireClient(ctx context.Context, backend string) (*clientLease, error) {
	if err := oc.acquireBackend(ctx, backend); err != nil {
		return nil, err
	}
	lease, err := oc.leaseClientAfterAdmission(backend)
	if err != nil {
		oc.releaseBackend(backend)
		return nil, err
	}
	if !oc.CircuitBreaker.IsHealthy(backend) {
		lease.Release()
		oc.releaseBackend(backend)
		return nil, ErrBackendUnavailable
	}
	return lease, nil
}

func (oc *OpenAIClients) leaseClientAfterAdmission(backend string) (*clientLease, error) {
	oc.mu.Lock()
	if oc.closed {
		oc.mu.Unlock()
		return nil, ErrClientsClosed
	}
	gen := oc.generations[backend]
	if gen != nil {
		gen.leases++
	}
	oc.mu.Unlock()
	if gen == nil {
		return nil, errors.New("no client available")
	}
	return &clientLease{oc: oc, backend: backend, gen: gen}, nil
}

func (oc *OpenAIClients) releaseAttempt(backend string, lease *clientLease) {
	if lease != nil {
		lease.Release()
	}
	oc.releaseBackend(backend)
}

func (oc *OpenAIClients) acquireBackend(ctx context.Context, backend string) error {
	limiter := oc.backendLimiter(backend)
	select {
	case limiter <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (oc *OpenAIClients) releaseBackend(backend string) {
	limiter := oc.backendLimiter(backend)
	if limiter != nil {
		<-limiter
	}
}

func (oc *OpenAIClients) backendLimiter(backend string) chan struct{} {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	if oc.limiters == nil {
		oc.limiters = make(map[string]chan struct{})
	}
	if limiter := oc.limiters[backend]; limiter != nil {
		return limiter
	}
	workers := 1
	if cfg, ok := oc.configs[backend]; ok {
		workers = cfg.GetWorkers()
	}
	limiter := make(chan struct{}, workers)
	oc.limiters[backend] = limiter
	return limiter
}

func (oc *OpenAIClients) closeRetiredGenerationLocked(gen *clientGeneration) {
	if gen == nil || !gen.retired || gen.leases != 0 || gen.closed {
		return
	}
	gen.closed = true
	if gen.client != nil {
		_ = gen.client.Close()
	}
}
