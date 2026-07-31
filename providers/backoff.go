package providers

import (
	"context"
	"math/rand/v2"
	"time"
)

// backoffJitter draws a full-jitter delay uniformly in [0, slot). It is a
// package-level seam so tests can make retry backoff deterministic; the default
// uses math/rand/v2's top-level generator, which is safe for concurrent use
// across llambo's parallel retries (no shared *rand.Rand to race on).
var backoffJitter = func(slot time.Duration) time.Duration {
	if slot <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(slot)))
}

// backoffDelay returns an AWS "full jitter" exponential backoff for the given
// retry attempt: a random draw in [0, min(cap, base*2^attempt)]. base is the
// first entry of the schedule, cap the last. Jitter breaks lockstep so the
// concurrent retries llambo fans out across backends don't form a thundering
// herd. Returns 0 when the schedule is empty (no backoff configured).
func backoffDelay(schedule []time.Duration, attempt int) time.Duration {
	if len(schedule) == 0 || attempt < 0 {
		return 0
	}
	base := schedule[0]
	capDelay := schedule[len(schedule)-1]
	slot := base << attempt // base * 2^attempt
	if slot <= 0 || slot > capDelay {
		// overflow from a large attempt, or grown past the cap
		slot = capDelay
	}
	return backoffJitter(slot)
}

// waitChatBackoff sleeps for a full-jitter backoff before the next retry,
// returning early if the context is cancelled.
func waitChatBackoff(ctx context.Context, reqConfig ChatRequestConfig, attempt int) error {
	delay := backoffDelay(reqConfig.Backoffs, attempt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
