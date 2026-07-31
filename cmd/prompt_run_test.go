package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/dotcommander/llambo/providers"
)

func TestPromptRunProviderAdmissionHonorsWorkersAndKeepsProvidersIndependent(t *testing.T) {
	t.Parallel()

	run := &promptRun{}
	providerA := providers.ProviderEntry{Name: "provider-a", Config: providers.Config{Workers: 1}}
	providerB := providers.ProviderEntry{Name: "provider-b", Config: providers.Config{Workers: 1}}

	releaseA, err := run.acquireProvider(context.Background(), providerA)
	if err != nil {
		t.Fatalf("acquire first provider-a slot: %v", err)
	}
	t.Cleanup(releaseA)
	limiter := run.providerLimiters[providerA.Name]
	if got, want := cap(limiter), 1; got != want {
		t.Fatalf("provider-a limiter capacity = %d, want workers=%d", got, want)
	}
	if got, want := len(limiter), 1; got != want {
		t.Fatalf("provider-a active admissions = %d, want %d", got, want)
	}

	// The saturated provider must respect its one-worker limit, while another
	// provider can still run in parallel.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := run.acquireProvider(canceled, providerA); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire saturated provider-a = %v, want context canceled", err)
	}

	releaseB, err := run.acquireProvider(context.Background(), providerB)
	if err != nil {
		t.Fatalf("acquire independent provider-b slot: %v", err)
	}
	defer releaseB()
}
