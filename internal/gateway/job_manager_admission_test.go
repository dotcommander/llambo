package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
)

func TestJobManager_CreateJob(t *testing.T) {
	t.Parallel()
	// Use a blocking mock so job doesn't complete before we check initial state
	queue := &flexibleJobQueue{
		workerCount: 2,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			// Block until jobs channel is closed (job cancelled or test ends)
			for range jobs {
				// Don't send results - just drain
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
	resp := job.ToResponse()

	// Verify job properties
	if resp.JobID == "" {
		t.Error("expected non-empty job ID")
	}
	if resp.Status != string(JobStatusPending) && resp.Status != string(JobStatusProcessing) {
		t.Errorf("expected status %q or %q, got %q", JobStatusPending, JobStatusProcessing, resp.Status)
	}
	if resp.Total != 2 {
		t.Errorf("expected Total=2, got %d", resp.Total)
	}
	if resp.Completed != 0 {
		t.Errorf("expected Completed=0, got %d", resp.Completed)
	}
	if resp.Failed != 0 {
		t.Errorf("expected Failed=0, got %d", resp.Failed)
	}

	// Verify ID format
	if len(resp.JobID) < 5 || resp.JobID[:4] != "job-" {
		t.Errorf("expected job ID to start with 'job-', got %q", resp.JobID)
	}
}

func TestJobManager_GetJob(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{workerCount: 2}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	// Unknown ID returns nil
	job := manager.GetJob("nonexistent")
	if job != nil {
		t.Error("expected nil for unknown job ID")
	}
}

func TestJobManager_GetJob_Exists(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{workerCount: 2}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	requests := []JobRequest{
		{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
	}

	created := manager.CreateJob(t.Context(), requests, "System")

	// Retrieve the same job
	retrieved := manager.GetJob(created.ID)
	if retrieved == nil {
		t.Fatal("expected to find created job")
	}
	if retrieved.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, retrieved.ID)
	}
}

func TestJobManager_CancelJob_Unknown(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{workerCount: 1}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	ok := manager.CancelJob("nonexistent-job-id")
	if ok {
		t.Error("expected CancelJob to fail for unknown job ID")
	}
}

func TestJobManager_CreateJobIfCapacity_Saturated(t *testing.T) {
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
	manager.SetMaxActiveJobs(1)

	requests := []JobRequest{{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}}}

	first, err := manager.CreateJobIfCapacity(t.Context(), requests, "System")
	if err != nil {
		t.Fatalf("expected first job creation to succeed, got %v", err)
	}
	if first == nil {
		t.Fatal("expected first job to be non-nil")
	}

	time.Sleep(20 * time.Millisecond) // allow first job to become active

	second, err := manager.CreateJobIfCapacity(t.Context(), requests, "System")
	if err == nil {
		t.Fatal("expected saturation error for second job")
	}
	if second != nil {
		t.Fatal("expected nil second job when saturated")
	}

	close(blockCh)
}

func TestJobManager_Stats(t *testing.T) {
	t.Parallel()
	blockCh := make(chan struct{})
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			<-blockCh // Wait to be unblocked
			for job := range jobs {
				results <- providers.Result{ID: job.ID, Content: "done"}
			}
		},
	}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	// Create jobs
	requests := []JobRequest{
		{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
	}

	manager.CreateJob(t.Context(), requests, "System")
	manager.CreateJob(t.Context(), requests, "System")

	// Wait a moment for jobs to transition
	time.Sleep(10 * time.Millisecond)

	total, pending, processing, completed, failed, cancelled := manager.Stats()

	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	// Jobs should be processing or pending
	if pending+processing != 2 {
		t.Errorf("expected pending+processing=2, got pending=%d processing=%d", pending, processing)
	}
	if completed != 0 {
		t.Errorf("expected completed=0, got %d", completed)
	}
	if failed != 0 {
		t.Errorf("expected failed=0, got %d", failed)
	}
	if cancelled != 0 {
		t.Errorf("expected cancelled=0, got %d", cancelled)
	}

	close(blockCh)
}

func TestJobManager_Concurrent(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{
		workerCount: 4,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			for job := range jobs {
				results <- providers.Result{ID: job.ID, Content: "done"}
			}
		},
	}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	var wg sync.WaitGroup
	numGoroutines := 10
	jobsPerGoroutine := 5

	createdJobs := make(chan *Job, numGoroutines*jobsPerGoroutine)

	// Concurrently create jobs
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < jobsPerGoroutine; j++ {
				requests := []JobRequest{
					{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
				}
				job := manager.CreateJob(t.Context(), requests, "System")
				createdJobs <- job
			}
		}()
	}

	wg.Wait()
	close(createdJobs)

	// Verify all jobs have unique IDs
	seenIDs := make(map[string]bool)
	for job := range createdJobs {
		if seenIDs[job.ID] {
			t.Errorf("duplicate job ID: %s", job.ID)
		}
		seenIDs[job.ID] = true
	}

	expectedJobs := numGoroutines * jobsPerGoroutine
	if len(seenIDs) != expectedJobs {
		t.Errorf("expected %d unique jobs, got %d", expectedJobs, len(seenIDs))
	}

	// Verify all jobs are tracked in manager
	total, _, _, _, _, _ := manager.Stats()
	if total != expectedJobs {
		t.Errorf("expected total=%d, got %d", expectedJobs, total)
	}
}

func TestJobManager_CleanupOldJobs(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
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

	// Manually set old timestamp
	job.mu.Lock()
	job.UpdatedAt = time.Now().Add(-2 * time.Hour)
	job.mu.Unlock()

	// Cleanup with 1 hour max age
	removed := manager.CleanupOldJobs(1 * time.Hour)
	if removed != 1 {
		t.Errorf("expected 1 job removed, got %d", removed)
	}

	// Job should be gone
	if manager.GetJob(job.ID) != nil {
		t.Error("expected job to be removed after cleanup")
	}
}
