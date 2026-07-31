package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// flexibleJobQueue implements providers.JobQueue with customizable behavior for testing
type flexibleJobQueue struct {
	processFunc       func(ctx context.Context, jobs []providers.Job) []providers.Result
	processStreamFunc func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted)
	workerCount       int
	backendNames      []string
}

func (m *flexibleJobQueue) Process(ctx context.Context, jobs []providers.Job) []providers.Result {
	if m.processFunc != nil {
		return m.processFunc(ctx, jobs)
	}
	return nil
}

func (m *flexibleJobQueue) ProcessStream(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
	if m.processStreamFunc != nil {
		m.processStreamFunc(ctx, jobs, results, maxWorkers, started)
		return
	}
	// Default: drain jobs channel and send empty results
	for job := range jobs {
		results <- providers.Result{
			ID:      job.ID,
			Content: "mock response",
		}
	}
}

func (m *flexibleJobQueue) WorkerCount() int                             { return m.workerCount }
func (m *flexibleJobQueue) BackendNames() []string                       { return m.backendNames }
func (m *flexibleJobQueue) Shutdown()                                    {}
func (m *flexibleJobQueue) GetCircuitBreaker() *providers.CircuitBreaker { return nil }

func TestJobManager_ProcessJob(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{
		workerCount: 2,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			for job := range jobs {
				results <- providers.Result{
					ID:       job.ID,
					Content:  "response for " + job.ID,
					Backend:  "openai",
					Model:    "gpt-4",
					Duration: 100 * time.Millisecond,
				}
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

	// Verify final state
	job.mu.RLock()
	defer job.mu.RUnlock()

	if job.Status != JobStatusCompleted {
		t.Errorf("expected status %q, got %q", JobStatusCompleted, job.Status)
	}
	if job.Completed != 2 {
		t.Errorf("expected Completed=2, got %d", job.Completed)
	}
	if job.Failed != 0 {
		t.Errorf("expected Failed=0, got %d", job.Failed)
	}
	if len(job.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(job.Results))
	}
}

func TestJobManager_ProcessJob_WithFailures(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{
		workerCount: 2,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			for job := range jobs {
				if job.ID == "req-2" {
					results <- providers.Result{
						ID:    job.ID,
						Error: errors.New("backend error"),
					}
				} else {
					results <- providers.Result{
						ID:      job.ID,
						Content: "success",
					}
				}
			}
		},
	}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	requests := []JobRequest{
		{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
		{ID: "req-2", Messages: []Message{{Role: "user", Content: "World"}}},
		{ID: "req-3", Messages: []Message{{Role: "user", Content: "Test"}}},
	}

	job := manager.CreateJob(t.Context(), requests, "System")

	// Wait for job to complete
	timeout := time.After(2 * time.Second)
	for {
		job.mu.RLock()
		status := job.Status
		job.mu.RUnlock()

		if status.IsTerminal() {
			break
		}

		select {
		case <-timeout:
			t.Fatal("timed out waiting for job to complete")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	job.mu.RLock()
	defer job.mu.RUnlock()

	// Job should be partial_failure since some (not all) requests failed
	if job.Status != JobStatusPartialFailure {
		t.Errorf("expected status %q, got %q", JobStatusPartialFailure, job.Status)
	}
	if job.Completed != 2 {
		t.Errorf("expected Completed=2, got %d", job.Completed)
	}
	if job.Failed != 1 {
		t.Errorf("expected Failed=1, got %d", job.Failed)
	}

	// Check individual result statuses
	failedCount := 0
	for _, r := range job.Results {
		if r.Status == string(ResultStatusFailed) {
			failedCount++
			if r.Error == "" {
				t.Error("expected error message for failed result")
			}
		}
	}
	if failedCount != 1 {
		t.Errorf("expected 1 failed result, got %d", failedCount)
	}
}

func TestJobManager_ProcessJob_Empty(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			for range jobs {
				// No jobs to process
			}
		},
	}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	requests := []JobRequest{}

	job := manager.CreateJob(t.Context(), requests, "System")

	// Wait for job to complete
	timeout := time.After(2 * time.Second)
	for {
		job.mu.RLock()
		status := job.Status
		job.mu.RUnlock()

		if status.IsTerminal() {
			break
		}

		select {
		case <-timeout:
			t.Fatal("timed out waiting for job to complete")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	job.mu.RLock()
	defer job.mu.RUnlock()

	if job.Total != 0 {
		t.Errorf("expected Total=0, got %d", job.Total)
	}
	if job.Completed != 0 {
		t.Errorf("expected Completed=0, got %d", job.Completed)
	}
	if job.Failed != 0 {
		t.Errorf("expected Failed=0, got %d", job.Failed)
	}
	// Empty job: Failed==Total (0==0) triggers "failed" status per current implementation
	// This is a quirk: an empty job is considered "failed" because the condition
	// `job.Failed == job.Total` is true when both are 0
	if job.Status != JobStatusFailed {
		t.Errorf("expected status %q for empty job, got %q", JobStatusFailed, job.Status)
	}
}

func TestJobManager_AllFailures_StatusFailed(t *testing.T) {
	t.Parallel()
	queue := &flexibleJobQueue{
		workerCount: 2,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			for job := range jobs {
				results <- providers.Result{
					ID:    job.ID,
					Error: errors.New("all backends failed"),
				}
			}
		},
	}
	manager := NewJobManager(context.Background(), queue)
	defer manager.StopCleanup()

	requests := []JobRequest{
		{ID: "req-1", Messages: []Message{{Role: "user", Content: "Hello"}}},
		{ID: "req-2", Messages: []Message{{Role: "user", Content: "World"}}},
	}

	job := manager.CreateJob(t.Context(), requests, "System")

	// Wait for job to complete
	timeout := time.After(2 * time.Second)
	for {
		job.mu.RLock()
		status := job.Status
		job.mu.RUnlock()

		if status.IsTerminal() {
			break
		}

		select {
		case <-timeout:
			t.Fatal("timed out waiting for job to complete")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	job.mu.RLock()
	defer job.mu.RUnlock()

	// When all requests fail, job status should be "failed"
	if job.Status != JobStatusFailed {
		t.Errorf("expected status %q when all fail, got %q", JobStatusFailed, job.Status)
	}
	if job.Failed != 2 {
		t.Errorf("expected Failed=2, got %d", job.Failed)
	}
	if job.Completed != 0 {
		t.Errorf("expected Completed=0, got %d", job.Completed)
	}
}
