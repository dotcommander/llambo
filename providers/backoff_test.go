package providers

import (
	"testing"
	"time"
)

func TestBackoffDelay_FullJitterExponentialWithCap(t *testing.T) {
	// Overrides the package-level jitter seam; must not run in parallel.
	prev := backoffJitter
	// Identity jitter makes the computed slot ceiling directly observable.
	backoffJitter = func(slot time.Duration) time.Duration { return slot }
	t.Cleanup(func() { backoffJitter = prev })

	schedule := []time.Duration{1 * time.Second, 2 * time.Second}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},  // base*2^0 = 1s
		{1, 2 * time.Second},  // base*2^1 = 2s
		{2, 2 * time.Second},  // base*2^2 = 4s, capped at 2s
		{99, 2 * time.Second}, // shift overflow guarded, capped at 2s
	}
	for _, tc := range cases {
		if got := backoffDelay(schedule, tc.attempt); got != tc.want {
			t.Fatalf("backoffDelay(attempt=%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}

	if got := backoffDelay(nil, 0); got != 0 {
		t.Fatalf("backoffDelay(empty schedule) = %v, want 0", got)
	}
	if got := backoffDelay(schedule, -1); got != 0 {
		t.Fatalf("backoffDelay(negative attempt) = %v, want 0", got)
	}
}

func TestBackoffDelay_DefaultJitterStaysWithinSlot(t *testing.T) {
	t.Parallel()
	schedule := []time.Duration{1 * time.Second, 2 * time.Second}
	for attempt := 0; attempt < 5; attempt++ {
		for i := 0; i < 200; i++ {
			d := backoffDelay(schedule, attempt)
			if d < 0 || d > 2*time.Second {
				t.Fatalf("backoffDelay(attempt=%d) = %v, out of [0,2s]", attempt, d)
			}
		}
	}
}
