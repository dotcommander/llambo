package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dotcommander/llambo/providers"
)

var (
	// ErrJobManagerShuttingDown rejects new job admission after BeginShutdown.
	ErrJobManagerShuttingDown = errors.New("job manager is shutting down")
	// ErrJobManagerSaturated rejects admission when the active job limit is full.
	ErrJobManagerSaturated = errors.New("job manager is saturated")
)

// JobManager manages batch jobs with parallel processing.
//
// Lifecycle: NewJobManager starts a background cleanupLoop goroutine
// whose exit is governed by cleanupCtx. StopCleanup calls cleanupCancel
// (safe to call repeatedly — cancel is idempotent, unlike close(chan))
// and waits for cleanupLoop to exit via cleanupWG.
type JobManager struct {
	queue            providers.JobQueue // parallel job processing queue
	jobs             map[string]*Job
	maxActive        int
	maxRetained      int
	maxRetainedBytes int64
	activeJobs       int
	mu               sync.RWMutex
	cleanupCtx       context.Context
	cleanupCancel    context.CancelFunc
	cleanupWG        sync.WaitGroup
	shuttingDown     bool
	drainDone        chan struct{}
	drainClosed      bool
}

// NewJobManager creates a new job manager with automatic cleanup. The
// cleanup goroutine's lifetime is bounded by ctx — typically the
// server-lifecycle context — though StopCleanup can also cancel it directly.
func NewJobManager(ctx context.Context, queue providers.JobQueue) *JobManager {
	cleanupCtx, cancel := context.WithCancel(ctx)
	m := &JobManager{
		queue:       queue,
		jobs:        make(map[string]*Job),
		maxActive:   JobMaxActiveDefault,
		maxRetained: 1000, maxRetainedBytes: 67108864,
		cleanupCtx:    cleanupCtx,
		cleanupCancel: cancel,
		drainDone:     make(chan struct{}),
	}
	// Start background cleanup goroutine. Exit condition: cleanupCtx canceled.
	m.cleanupWG.Add(1)
	go m.cleanupLoop()
	return m
}

// SetMaxActiveJobs sets the active job admission limit (pending + processing).
// Values <= 0 disable the limit.
func (m *JobManager) SetMaxActiveJobs(limit int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxActive = limit
}

// MaxActiveJobs returns the current active job admission limit.
func (m *JobManager) MaxActiveJobs() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxActive
}

// cleanupLoop periodically removes old completed jobs.
// Exit condition: cleanupCtx canceled (via StopCleanup).
func (m *JobManager) cleanupLoop() {
	defer m.cleanupWG.Done()
	ticker := time.NewTicker(JobCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.CleanupOldJobs(JobMaxAge)
		case <-m.cleanupCtx.Done():
			return
		}
	}
}

// StopCleanup stops the background cleanup goroutine. Safe to call
// multiple times and from multiple goroutines — context.CancelFunc is
// idempotent, fixing the panic that close(chan) would cause on the
// second call.
func (m *JobManager) StopCleanup() {
	m.cleanupCancel()
	m.cleanupWG.Wait()
}

// BeginShutdown permanently stops new job admission, cancels every active
// job, and stops background cleanup. It is safe to call repeatedly.
func (m *JobManager) BeginShutdown() {
	m.mu.Lock()
	m.shuttingDown = true
	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.closeDrainIfIdleLocked()
	m.mu.Unlock()

	for _, job := range jobs {
		m.cancel(job)
	}
	m.StopCleanup()
}

// WaitForDrain waits for every job admitted before BeginShutdown to release
// its worker pool. A context deadline leaves the manager and its resources
// open so a later Shutdown call can retry the drain.
func (m *JobManager) WaitForDrain(ctx context.Context) error {
	select {
	case <-m.drainDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CreateJob creates and starts a new batch job.
// parent supplies request-scoped values (trace IDs, etc.); its
// cancellation does NOT cancel the job — batch jobs intentionally
// outlive the HTTP request that submits them. Use CancelJob to cancel.
func (m *JobManager) CreateJob(parent context.Context, requests []JobRequest, systemPrompt string) *Job {
	job, _ := m.createJob(parent, requests, systemPrompt, false)
	return job
}

// CreateJobIfCapacity creates a new job when admission capacity allows it.
// See CreateJob for the parent-context contract.
func (m *JobManager) CreateJobIfCapacity(parent context.Context, requests []JobRequest, systemPrompt string) (*Job, error) {
	return m.createJob(parent, requests, systemPrompt, true)
}

func (m *JobManager) createJob(parent context.Context, requests []JobRequest, systemPrompt string, enforceCapacity bool) (*Job, error) {
	return m.createTargetJob(parent, requests, systemPrompt, enforceCapacity, providers.ResolvedTarget{})
}
func (m *JobManager) createTargetJob(parent context.Context, requests []JobRequest, systemPrompt string, enforceCapacity bool, target providers.ResolvedTarget) (*Job, error) {
	id := generateID("job")
	encoded, _ := json.Marshal(CreateJobRequest{Requests: requests, SystemPrompt: systemPrompt, Model: target.Model()})
	queueJobs := make(chan providers.Job, len(requests))
	for candidate := range convertToTargetQueueJobs(requests, systemPrompt, target) {
		if preparer, ok := m.queue.(interface {
			PrepareJob(providers.Job) (providers.Job, error)
		}); ok {
			var err error
			candidate, err = preparer.PrepareJob(candidate)
			if err != nil {
				return nil, err
			}
		}
		queueJobs <- candidate
	}
	close(queueJobs)
	// Inherit parent's values (e.g. request IDs, deadlines for logging)
	// but NOT its cancellation: jobs are async background work that
	// outlives the submitting HTTP request.
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))

	job := &Job{
		ID:     id,
		target: target, requestBytes: int64(len(encoded)), queueJobs: queueJobs,
		Status:    JobStatusPending,
		Total:     len(requests),
		Results:   make([]JobResult, 0, len(requests)),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		cancel()
		return nil, ErrJobManagerShuttingDown
	}
	if enforceCapacity && m.maxActive > 0 && m.activeJobs >= m.maxActive {
		active := m.activeJobs
		m.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("%w: %d active jobs (limit %d)", ErrJobManagerSaturated, active, m.maxActive)
	}
	m.jobs[id] = job
	m.activeJobs++
	m.mu.Unlock()

	// Start processing in background
	go m.processJob(job, requests, systemPrompt)

	return job, nil
}

// GetJob retrieves a job by ID
func (m *JobManager) GetJob(id string) *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

// CancelJob cancels a running job.
//
// Non-blocking: returns as soon as the job context is cancelled. The
// processJob goroutine may still be draining its worker pool when this
// returns, so the admission counter has NOT yet been released and a
// subsequent GetJob may still observe "active" state for a brief window.
// Use CancelJobAndWait when callers need the slot fully released before
// proceeding.
func (m *JobManager) CancelJob(id string) bool {
	_, ok := m.cancelJob(id)
	return ok
}

// CancelJobAndWait cancels a job and blocks until processJob has exited
// (admission slot released, job.done closed) or ctx is done.
//
// Returns:
//   - (true, nil) on successful cancel + drain.
//   - (false, nil) when the job does not exist or is not cancellable.
//   - (true, ctx.Err()) when cancel succeeded but the wait exceeded ctx.
func (m *JobManager) CancelJobAndWait(ctx context.Context, id string) (bool, error) {
	done, ok := m.cancelJob(id)
	if !ok {
		return false, nil
	}
	select {
	case <-done:
		return true, nil
	case <-ctx.Done():
		return true, ctx.Err()
	}
}

// cancelJob is the shared implementation behind CancelJob and
// CancelJobAndWait. It returns the job.done channel so callers can wait
// for processJob to finish draining if they need to.
func (m *JobManager) cancelJob(id string) (<-chan struct{}, bool) {
	m.mu.RLock()
	job, ok := m.jobs[id]
	m.mu.RUnlock()

	if !ok {
		return nil, false
	}

	done, ok := m.cancel(job)
	if !ok {
		return nil, false
	}

	// Do not decrement the admission counter here: the ProcessStream
	// goroutine may still be draining its worker pool. processJob releases
	// the slot exactly once when that goroutine has fully exited.
	return done, true
}

// CancelAll cancels all running jobs
func (m *JobManager) CancelAll() {
	m.mu.RLock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.mu.RUnlock()

	for _, job := range jobs {
		m.cancel(job)
		// Slot release is deferred to processJob, which fires once the
		// ProcessStream goroutine for this job has fully exited.
	}
}

// cancel transitions only cancellable jobs to cancelled. Keeping this check
// under the job lock prevents a late queue close from overwriting cancellation
// with a completed or failed terminal state.
func (m *JobManager) cancel(job *Job) (<-chan struct{}, bool) {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.Status != JobStatusPending && job.Status != JobStatusProcessing {
		return nil, false
	}
	job.cancel()
	job.Status = JobStatusCancelled
	job.UpdatedAt = time.Now()
	return job.done, true
}

func (m *JobManager) closeDrainIfIdleLocked() {
	if m.shuttingDown && m.activeJobs == 0 && !m.drainClosed {
		close(m.drainDone)
		m.drainClosed = true
	}
}

// CleanupOldJobs removes jobs older than the given duration
func (m *JobManager) CleanupOldJobs(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.enforceRetentionLocked(time.Now().Add(-maxAge))
}

// Stats returns job statistics
func (m *JobManager) Stats() (total, pending, processing, completed, failed, cancelled int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total = len(m.jobs)
	for _, job := range m.jobs {
		job.mu.RLock()
		switch job.Status {
		case JobStatusPending:
			pending++
		case JobStatusProcessing:
			processing++
		case JobStatusCompleted:
			completed++
		case JobStatusFailed:
			failed++
		case JobStatusCancelled:
			cancelled++
		}
		job.mu.RUnlock()
	}
	return
}
