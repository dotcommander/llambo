package providers

import (
	"context"
	"errors"
	"testing"
)

func TestAcquireClient_CanceledWhileWaitingDoesNotAffectBreaker(t *testing.T) {
	t.Parallel()
	const backend = "openai"
	oc := &OpenAIClients{
		CircuitBreaker: NewCircuitBreaker([]string{backend}, nil),
		configs: map[string]Config{
			backend: {Workers: 1},
		},
		limiters: map[string]chan struct{}{backend: make(chan struct{}, 1)},
		generations: map[string]*clientGeneration{
			backend: {client: &fakeTextProvider{}},
		},
	}

	requireNoError(t, oc.acquireBackend(context.Background(), backend))
	t.Cleanup(func() { oc.releaseBackend(backend) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lease, err := oc.acquireClient(ctx, backend)
	if lease != nil {
		t.Fatal("canceled admission unexpectedly acquired a client lease")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("acquireClient error = %v, want context canceled", err)
	}
	if !oc.CircuitBreaker.IsHealthy(backend) {
		t.Fatal("canceled admission must not affect circuit-breaker health")
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
