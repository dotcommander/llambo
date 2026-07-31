package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
	"go.uber.org/goleak"
)

// TestProcessJob_NoGoroutineLeak asserts the inner ProcessStream goroutine in
// processJob exits on both of its documented termination triggers: the input
// channel draining (normal completion) and job.ctx cancellation. goleak.VerifyNone
// fails the test if any goroutine spawned during the run is still alive at the end.
func TestProcessJob_NoGoroutineLeak(t *testing.T) {
	t.Run("drain", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		queue := &flexibleJobQueue{
			workerCount: 2,
			processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
				// Drain input, emit a result per job, then return so the
				// inner goroutine's deferred closes fire (normal exit path).
				for j := range jobs {
					results <- providers.Result{ID: j.ID, Content: "ok"}
				}
			},
		}
		manager := NewJobManager(context.Background(), queue)
		defer manager.StopCleanup()

		requests := []JobRequest{
			{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
			{ID: "req-2", Messages: []Message{{Role: "user", Content: "World"}}},
		}
		job := manager.CreateJob(t.Context(), requests, "Be helpful")

		waitJobDone(t, job)
	})

	t.Run("cancel", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		queue := &flexibleJobQueue{
			workerCount: 2,
			processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
				// Block until ctx is cancelled (the cancellation exit path).
				<-ctx.Done()
			},
		}
		manager := NewJobManager(context.Background(), queue)
		defer manager.StopCleanup()

		requests := []JobRequest{
			{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
		}
		job := manager.CreateJob(t.Context(), requests, "Be helpful")

		// Cancel the job's context to trigger the cancellation exit path.
		job.cancel()
		waitJobDone(t, job)
	})
}

// waitJobDone blocks until processJob has closed job.done (slot released and the
// inner goroutine fully exited), failing the test if it does not happen promptly.
func waitJobDone(t *testing.T, job *Job) {
	t.Helper()
	select {
	case <-job.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for job.done; inner goroutine likely leaked")
	}
}
