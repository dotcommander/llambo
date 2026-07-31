package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// Job represents a batch processing job
type Job struct {
	ID        string
	Status    JobStatus
	Total     int
	Completed int
	Failed    int
	Results   []JobResult
	CreatedAt time.Time
	UpdatedAt time.Time

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex

	// done is closed by processJob once the ProcessStream goroutine has fully
	// exited. It is the single signal that the job has released its worker
	// pool, so the active-job admission slot can be reclaimed exactly once.
	done chan struct{}
}

// ToResponse converts Job to JobResponse with proper locking
func (j *Job) ToResponse() JobResponse {
	j.mu.RLock()
	defer j.mu.RUnlock()

	// Deep copy results to avoid race conditions
	results := make([]JobResult, len(j.Results))
	copy(results, j.Results)

	return JobResponse{
		JobID:     j.ID,
		Status:    string(j.Status),
		Total:     j.Total,
		Completed: j.Completed,
		Failed:    j.Failed,
		Results:   results,
		CreatedAt: j.CreatedAt.Unix(),
		UpdatedAt: j.UpdatedAt.Unix(),
	}
}

// convertToQueueJobs converts JobRequests to a channel of provider Jobs
func convertToQueueJobs(requests []JobRequest, systemPrompt string) chan providers.Job {
	queueJobs := make(chan providers.Job, len(requests))
	for _, req := range requests {
		userContent := ExtractUserContent(req.Messages)
		queueJobs <- providers.Job{
			ID:           req.ID,
			SystemPrompt: systemPrompt,
			UserContent:  userContent,
		}
	}
	close(queueJobs)
	return queueJobs
}

// collectResults processes results from the queue and updates job state
func (m *JobManager) collectResults(job *Job, results <-chan providers.Result) {
	for {
		select {
		case <-job.ctx.Done():
			// Job was cancelled
			return

		case result, ok := <-results:
			if !ok {
				// All results received - determine final status. The
				// admission slot is released by processJob, not here, so
				// cancelled and completed jobs follow one decrement path.
				job.mu.Lock()
				if job.Status == JobStatusCancelled {
					job.mu.Unlock()
					return
				}
				switch {
				case job.Failed == job.Total:
					job.Status = JobStatusFailed
				case job.Failed > 0:
					job.Status = JobStatusPartialFailure
				default:
					job.Status = JobStatusCompleted
				}
				job.UpdatedAt = time.Now()
				job.mu.Unlock()
				return
			}

			// Process result
			jobResult := JobResult{
				ID:         result.ID,
				Status:     string(ResultStatusCompleted),
				Content:    result.Content,
				Backend:    result.Backend,
				Model:      result.Model,
				DurationMs: result.Duration.Milliseconds(),
			}

			if result.Error != nil {
				jobResult.Status = string(ResultStatusFailed)
				jobResult.Error = upstreamErrorDetail(result.Error).Message
				jobResult.Content = ""
			}

			job.mu.Lock()
			if job.Status == JobStatusCancelled {
				job.mu.Unlock()
				return
			}
			job.Results = append(job.Results, jobResult)
			if result.Error != nil {
				job.Failed++
			} else {
				job.Completed++
			}
			job.UpdatedAt = time.Now()
			job.mu.Unlock()
		}
	}
}

// processJob processes a job using the BackendQueue.
//
// It owns the job lifecycle from creation to slot release: the admission
// counter is decremented exactly once, only after the ProcessStream goroutine
// (and its worker pool) has fully exited. This prevents a cancelled job from
// freeing its slot while its goroutine is still draining — which would let
// admission over-count and outlive the job.
func (m *JobManager) processJob(job *Job, requests []JobRequest, systemPrompt string) {
	// Single done signal + slot release, regardless of completion or cancel.
	defer func() {
		m.decrementActiveJobs()
		close(job.done)
	}()

	job.mu.Lock()
	if job.Status != JobStatusPending {
		job.mu.Unlock()
		return
	}
	job.Status = JobStatusProcessing
	job.UpdatedAt = time.Now()
	job.mu.Unlock()

	queueJobs := convertToQueueJobs(requests, systemPrompt)

	// Create results channel
	results := make(chan providers.Result, len(requests))

	// Process through queue with streaming results.
	streamDone := make(chan struct{})
	// Exit: ProcessStream returns when job.ctx is cancelled or queueJobs drains;
	// its return is the sole termination trigger, after which the deferred closes fire.
	go func() {
		defer close(results)
		defer close(streamDone)
		m.queue.ProcessStream(job.ctx, queueJobs, results, 0, nil)
	}()

	m.collectResults(job, results)

	// collectResults may return early on cancellation while ProcessStream is
	// still unwinding. Wait for the goroutine to exit so the slot is released
	// only when the worker pool is truly idle.
	<-streamDone
}

func (m *JobManager) decrementActiveJobs() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeJobs > 0 {
		m.activeJobs--
	}
	m.closeDrainIfIdleLocked()
}
