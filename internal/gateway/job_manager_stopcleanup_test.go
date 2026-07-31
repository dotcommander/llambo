package gateway

import (
	"context"
	"sync"
	"testing"
)

// TestJobManager_StopCleanup_Concurrent verifies that StopCleanup is
// safe to call repeatedly and concurrently — the previous implementation
// (close(chan struct{})) panicked on the second call with "close of
// closed channel."
func TestJobManager_StopCleanup_Concurrent(t *testing.T) {
	t.Parallel()

	queue := newHandlerMockJobQueue([]string{"mock"})
	manager := NewJobManager(context.Background(), queue)

	const callers = 100
	var wg sync.WaitGroup
	wg.Add(callers)
	start := make(chan struct{})
	for range callers {
		go func() {
			defer wg.Done()
			<-start
			manager.StopCleanup()
		}()
	}
	close(start)
	wg.Wait()

	// One more call should still be safe.
	manager.StopCleanup()
}
