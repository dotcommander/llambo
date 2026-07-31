package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/dotcommander/llambo/providers"
)

func TestJobManager_BeginShutdownRejectsAdmissionAndDrains(t *testing.T) {
	streamStarted := make(chan struct{})
	releaseStream := make(chan struct{})
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			close(streamStarted)
			<-ctx.Done()
			<-releaseStream
		},
	}
	manager := NewJobManager(context.Background(), queue)
	t.Cleanup(manager.StopCleanup)

	requests := []JobRequest{{ID: "req", Messages: []Message{{Role: "user", Content: "hello"}}}}
	job, err := manager.CreateJobIfCapacity(t.Context(), requests, "system")
	if err != nil {
		t.Fatalf("CreateJobIfCapacity() error = %v", err)
	}
	<-streamStarted

	manager.BeginShutdown()
	if _, err := manager.CreateJobIfCapacity(t.Context(), requests, "system"); !errors.Is(err, ErrJobManagerShuttingDown) {
		t.Fatalf("CreateJobIfCapacity() error = %v, want ErrJobManagerShuttingDown", err)
	}
	if job := manager.CreateJob(t.Context(), requests, "system"); job != nil {
		t.Fatal("CreateJob() admitted a job after BeginShutdown")
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := manager.WaitForDrain(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitForDrain() error = %v, want context.Canceled", err)
	}
	select {
	case <-job.done:
		t.Fatal("job completed before its stream exited")
	default:
	}

	close(releaseStream)
	if err := manager.WaitForDrain(t.Context()); err != nil {
		t.Fatalf("WaitForDrain() error = %v", err)
	}
}

func TestJobManager_AdmissionErrorsRemainDistinct(t *testing.T) {
	queue := &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			<-ctx.Done()
		},
	}
	manager := NewJobManager(context.Background(), queue)
	t.Cleanup(manager.StopCleanup)
	manager.SetMaxActiveJobs(1)

	requests := []JobRequest{{ID: "req", Messages: []Message{{Role: "user", Content: "hello"}}}}
	if _, err := manager.CreateJobIfCapacity(t.Context(), requests, "system"); err != nil {
		t.Fatalf("first CreateJobIfCapacity() error = %v", err)
	}
	if _, err := manager.CreateJobIfCapacity(t.Context(), requests, "system"); !errors.Is(err, ErrJobManagerSaturated) {
		t.Fatalf("second CreateJobIfCapacity() error = %v, want ErrJobManagerSaturated", err)
	}

	manager.BeginShutdown()
	if _, err := manager.CreateJobIfCapacity(t.Context(), requests, "system"); !errors.Is(err, ErrJobManagerShuttingDown) {
		t.Fatalf("CreateJobIfCapacity() after BeginShutdown error = %v, want ErrJobManagerShuttingDown", err)
	}
	if err := manager.WaitForDrain(t.Context()); err != nil {
		t.Fatalf("WaitForDrain() error = %v", err)
	}
}

func TestJobManager_CancelledJobKeepsTerminalStatusAndSanitizesResult(t *testing.T) {
	manager := NewJobManager(context.Background(), &flexibleJobQueue{})
	t.Cleanup(manager.StopCleanup)
	job := &Job{
		Status:  JobStatusCancelled,
		Total:   1,
		Results: make([]JobResult, 0, 1),
		ctx:     context.Background(),
	}
	results := make(chan providers.Result, 1)
	results <- providers.Result{ID: "req", Error: errors.New("upstream secret")}
	close(results)

	manager.collectResults(job, results)
	if job.Status != JobStatusCancelled {
		t.Fatalf("status = %q, want %q", job.Status, JobStatusCancelled)
	}
	if len(job.Results) != 0 {
		t.Fatalf("cancelled job retained late results: %#v", job.Results)
	}

	job.Status = JobStatusProcessing
	results = make(chan providers.Result, 1)
	results <- providers.Result{ID: "req", Error: errors.New("upstream secret")}
	close(results)
	manager.collectResults(job, results)
	if len(job.Results) != 1 {
		t.Fatalf("results = %#v, want one failed result", job.Results)
	}
	if got := job.Results[0].Error; got != "Upstream provider request failed" {
		t.Fatalf("stored error = %q, want fixed upstream message", got)
	}
}
