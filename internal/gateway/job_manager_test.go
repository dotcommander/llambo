package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
)

func TestJobManager_CancelJob(t *testing.T) {
	t.Parallel()
	// Create a blocking queue that waits for signal
	blockCh := make(chan struct{})
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			// Block until signaled
			<-blockCh
			for job := range jobs {
				results <- providers.Result{ID: job.ID, Content: "done"}
			}
		},
	}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	requests := []JobRequest{
		{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
	}

	job := manager.CreateJob(t.Context(), requests, "System")

	// Wait briefly for job to start processing
	time.Sleep(10 * time.Millisecond)

	// Cancel while still pending or processing
	ok := manager.CancelJob(job.ID)
	if !ok {
		t.Error("expected CancelJob to succeed")
	}

	job.mu.RLock()
	status := job.Status
	job.mu.RUnlock()

	if status != JobStatusCancelled {
		t.Errorf("expected status %q, got %q", JobStatusCancelled, status)
	}

	// Unblock the queue
	close(blockCh)
}

func TestJobManager_CancelJob_Processing(t *testing.T) {
	t.Parallel()
	processingStarted := make(chan struct{})
	blockCh := make(chan struct{})

	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			close(processingStarted) // Signal that processing has started
			<-blockCh                // Wait to be unblocked
			for job := range jobs {
				results <- providers.Result{ID: job.ID, Content: "done"}
			}
		},
	}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	requests := []JobRequest{
		{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
	}

	job := manager.CreateJob(t.Context(), requests, "System")

	// Wait for processing to start
	<-processingStarted

	// Give a moment for status transition
	time.Sleep(5 * time.Millisecond)

	job.mu.RLock()
	currentStatus := job.Status
	job.mu.RUnlock()

	if currentStatus != JobStatusProcessing {
		t.Logf("warning: expected status %q, got %q (may be race condition)", JobStatusProcessing, currentStatus)
	}

	// Cancel the processing job
	ok := manager.CancelJob(job.ID)
	if !ok {
		t.Error("expected CancelJob to succeed for processing job")
	}

	job.mu.RLock()
	status := job.Status
	job.mu.RUnlock()

	if status != JobStatusCancelled {
		t.Errorf("expected status %q, got %q", JobStatusCancelled, status)
	}

	close(blockCh)
}

func TestJobManager_CancelJob_PropagatesContextToQueue(t *testing.T) {
	t.Parallel()
	ctxCancelled := make(chan struct{})
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			// Keep queue active until cancellation is propagated.
			<-ctx.Done()
			close(ctxCancelled)
		},
	}

	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	job := manager.CreateJob(t.Context(), []JobRequest{{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}}}, "System")

	// Let processing goroutine start.
	time.Sleep(20 * time.Millisecond)

	if ok := manager.CancelJob(job.ID); !ok {
		t.Fatal("expected cancel to succeed")
	}

	select {
	case <-ctxCancelled:
		// expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected queue context cancellation to propagate")
	}
}

// TestJobManager_CancelJob_SlotReleasedAfterStreamExits verifies the admission
// slot is reclaimed only once the ProcessStream goroutine has fully exited.
// Cancelling must not decrement the active-job counter eagerly: if it did, a
// new job could be admitted while the cancelled job's worker pool is still
// draining, over-counting capacity.
func TestJobManager_CancelJob_SlotReleasedAfterStreamExits(t *testing.T) {
	t.Parallel()
	releaseStream := make(chan struct{})
	streamExited := make(chan struct{})
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			<-ctx.Done()        // observe cancellation
			<-releaseStream     // stay in flight until the test releases us
			close(streamExited) // ProcessStream goroutine is now exiting
		},
	}

	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()
	manager.SetMaxActiveJobs(1)

	requests := []JobRequest{{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}}}

	job, err := manager.CreateJobIfCapacity(t.Context(), requests, "System")
	if err != nil {
		t.Fatalf("expected first job creation to succeed, got %v", err)
	}

	time.Sleep(20 * time.Millisecond) // let processJob start the stream goroutine

	if ok := manager.CancelJob(job.ID); !ok {
		t.Fatal("expected cancel to succeed")
	}

	// While the ProcessStream goroutine is still in flight, the slot must
	// remain held: a new job cannot be admitted.
	if _, err := manager.CreateJobIfCapacity(t.Context(), requests, "System"); err == nil {
		t.Fatal("expected saturation error: slot must stay held until stream exits")
	}

	// Release the stream goroutine; processJob then decrements the slot.
	close(releaseStream)
	<-streamExited
	<-job.done // processJob has released the slot

	// Slot is now free: a new job must be admissible.
	next, err := manager.CreateJobIfCapacity(t.Context(), requests, "System")
	if err != nil {
		t.Fatalf("expected admission after slot release, got %v", err)
	}
	if next == nil {
		t.Fatal("expected non-nil job after slot release")
	}
	manager.CancelJob(next.ID)
}

// TestJobManager_CancelJobAndWait_SlotReleasedBeforeReturn verifies the
// blocking cancel variant: when CancelJobAndWait returns successfully, the
// admission slot must already be free.
func TestJobManager_CancelJobAndWait_SlotReleasedBeforeReturn(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			<-ctx.Done() // exit promptly once cancelled
		},
	}

	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()
	manager.SetMaxActiveJobs(1)

	requests := []JobRequest{{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}}}

	job, err := manager.CreateJobIfCapacity(t.Context(), requests, "System")
	if err != nil {
		t.Fatalf("expected first job creation to succeed, got %v", err)
	}

	time.Sleep(20 * time.Millisecond) // let processJob start

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	ok, err := manager.CancelJobAndWait(ctx, job.ID)
	if err != nil {
		t.Fatalf("CancelJobAndWait returned err: %v", err)
	}
	if !ok {
		t.Fatal("expected CancelJobAndWait to succeed")
	}

	// Slot must be free immediately on return — no sleeps, no polling.
	next, err := manager.CreateJobIfCapacity(t.Context(), requests, "System")
	if err != nil {
		t.Fatalf("expected admission immediately after CancelJobAndWait, got %v", err)
	}
	if next == nil {
		t.Fatal("expected non-nil job after CancelJobAndWait returned")
	}

	// And the done channel of the cancelled job must already be closed.
	select {
	case <-job.done:
	default:
		t.Fatal("expected job.done closed before CancelJobAndWait returned")
	}

	manager.CancelJob(next.ID)
}

// TestJobManager_CancelJobAndWait_CtxTimeout verifies that the caller-supplied
// context bounds the wait.
func TestJobManager_CancelJobAndWait_CtxTimeout(t *testing.T) {
	t.Parallel()
	releaseStream := make(chan struct{})
	defer close(releaseStream)
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			<-ctx.Done()
			<-releaseStream // refuse to exit until the test releases
		},
	}

	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	job := manager.CreateJob(t.Context(), []JobRequest{{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}}}, "System")
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	ok, err := manager.CancelJobAndWait(ctx, job.ID)
	if !ok {
		t.Fatal("expected cancel to be initiated")
	}
	if err == nil {
		t.Fatal("expected ctx.Err on timeout")
	}
}

// TestJobManager_CancelJobAndWait_UnknownJob verifies that requesting cancel
// on a non-existent id returns (false, nil) without waiting.
func TestJobManager_CancelJobAndWait_UnknownJob(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{workerCount: 1}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	ok, err := manager.CancelJobAndWait(t.Context(), "no-such-job")
	if ok {
		t.Fatal("expected ok=false for unknown job")
	}
	if err != nil {
		t.Fatalf("expected nil err for unknown job, got %v", err)
	}
}

func TestJobManager_CancelJob_Completed(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			// Process immediately
			for job := range jobs {
				results <- providers.Result{ID: job.ID, Content: "done"}
			}
		},
	}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	requests := []JobRequest{
		{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
	}

	job := manager.CreateJob(t.Context(), requests, "System")

	// Wait for job to complete
	timeout := time.After(2 * time.Second)
	for {
		job.mu.RLock()
		status := job.Status
		job.mu.RUnlock()

		if status == JobStatusCompleted {
			break
		}

		select {
		case <-timeout:
			t.Fatal("timed out waiting for job to complete")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Try to cancel completed job
	ok := manager.CancelJob(job.ID)
	if ok {
		t.Error("expected CancelJob to fail for completed job")
	}

	// Status should still be completed
	job.mu.RLock()
	status := job.Status
	job.mu.RUnlock()

	if status != JobStatusCompleted {
		t.Errorf("expected status %q, got %q", JobStatusCompleted, status)
	}
}

func TestJobManager_CancelAll(t *testing.T) {
	t.Parallel()
	blockCh := make(chan struct{})
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			<-blockCh
			for job := range jobs {
				results <- providers.Result{ID: job.ID, Content: "done"}
			}
		},
	}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	requests := []JobRequest{
		{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
	}

	job1 := manager.CreateJob(t.Context(), requests, "System")
	job2 := manager.CreateJob(t.Context(), requests, "System")

	time.Sleep(10 * time.Millisecond)

	manager.CancelAll()

	job1.mu.RLock()
	status1 := job1.Status
	job1.mu.RUnlock()

	job2.mu.RLock()
	status2 := job2.Status
	job2.mu.RUnlock()

	if status1 != JobStatusCancelled {
		t.Errorf("expected job1 status %q, got %q", JobStatusCancelled, status1)
	}
	if status2 != JobStatusCancelled {
		t.Errorf("expected job2 status %q, got %q", JobStatusCancelled, status2)
	}

	close(blockCh)
}
