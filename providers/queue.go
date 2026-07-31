package providers

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/sourcegraph/conc/pool"
)

// JobQueue is the interface for parallel job processing queues
type JobQueue interface {
	Process(ctx context.Context, jobs []Job) []Result
	ProcessStream(ctx context.Context, jobs <-chan Job, results chan<- Result, maxWorkers int, started chan<- JobStarted)
	WorkerCount() int
	BackendNames() []string
	Shutdown()
	GetCircuitBreaker() *CircuitBreaker
}

// Ensure BackendQueue implements JobQueue
var _ JobQueue = (*BackendQueue)(nil)

// BackendQueue orchestrates parallel requests across all backends
// Uses conc for simplified worker pool management
type BackendQueue struct {
	oc       *OpenAIClients // holds clients, backends, circuit breaker
	configs  map[string]Config
	counter  uint64       // atomic counter for round-robin backend selection
	executor ChatExecutor // nil = use default real executor
}

// Job is work to be processed
type Job struct {
	ID           string
	SystemPrompt string
	UserContent  string
}

// Result is completed work
type Result struct {
	ID       string
	Content  string
	Backend  string
	Model    string
	Duration time.Duration
	Error    error
	Usage    *LLMUsage // token usage and cost (may be nil)
}

// JobStarted signals when a job begins processing
type JobStarted struct {
	ID      string
	Backend string
	Model   string
}

// NewBackendQueue creates queue with configured backends using shared OpenAIClients
// This ensures both OpenAIProvider and BackendQueue share the same circuit breaker state
func NewBackendQueue(oc *OpenAIClients, configs map[string]Config) *BackendQueue {
	return &BackendQueue{
		oc:      oc,
		configs: configs,
	}
}

// SetExecutor sets a custom executor for testing
func (q *BackendQueue) SetExecutor(e ChatExecutor) {
	q.executor = e
}

// nextBackend returns the next healthy backend using round-robin
// Falls back to any backend if all are unhealthy (gives them a chance to recover)
func (q *BackendQueue) nextBackend() Backend {
	healthy := q.oc.GetHealthyBackends()
	idx := atomic.AddUint64(&q.counter, 1) - 1

	if len(healthy) > 0 {
		return healthy[idx%uint64(len(healthy))]
	}

	// All backends unhealthy - return round-robin choice anyway (allows recovery).
	// Empty Backends would make the modulo panic with divide-by-zero.
	if len(q.oc.Backends) == 0 {
		return Backend{}
	}
	return q.oc.Backends[idx%uint64(len(q.oc.Backends))]
}

func (q *BackendQueue) executeJob(ctx context.Context, job Job, plan chatExecutionPlan) Result {
	outcome, err := q.coordinator().execute(ctx, plan, job.SystemPrompt, job.UserContent, nil, q.executeChatAttempt)
	return Result{
		ID:       job.ID,
		Content:  outcome.result.content,
		Backend:  outcome.provider.name,
		Model:    outcome.provider.cfg.Model,
		Duration: outcome.result.duration,
		Error:    err,
		Usage:    outcome.result.usage,
	}
}

// Process runs jobs through all backends in parallel using conc pool.
// ctx must be non-nil; callers should pass the caller's context.
func (q *BackendQueue) Process(ctx context.Context, jobs []Job) []Result {
	p := pool.NewWithResults[Result]().WithMaxGoroutines(q.WorkerCount())

	for _, job := range jobs {
		job := job // capture for closure
		p.Go(func() Result {
			plan, err := q.planJob(job)
			if err != nil {
				return Result{ID: job.ID, Error: err}
			}
			return q.executeJob(ctx, job, plan)
		})
	}

	return p.Wait()
}

// ProcessStream processes jobs from channel and streams results as they complete.
// maxWorkers limits concurrency (0 = use default WorkerCount).
// ctx must be non-nil; callers should pass the caller's context.
//
// Channel ownership: ProcessStream is the sole producer-side closer of
// `started` (when non-nil). The caller must NOT close `started` — doing
// so would race the producer goroutines. After ProcessStream returns,
// callers may range-loop on `started` and receive until close. Pass nil
// when started notifications are not needed.
func (q *BackendQueue) ProcessStream(ctx context.Context, jobs <-chan Job, results chan<- Result, maxWorkers int, started chan<- JobStarted) {
	// Close `started` exactly once — only here, after every worker that
	// could send on it has finished (p.Wait() returns). This is the sole
	// close site for the started channel; callers must not close it.
	if started != nil {
		defer close(started)
	}

	workers := q.WorkerCount()
	if maxWorkers > 0 && maxWorkers < workers {
		workers = maxWorkers
	}
	p := pool.New().WithMaxGoroutines(workers)

	for {
		select {
		case <-ctx.Done():
			p.Wait()
			return
		case job, ok := <-jobs:
			if !ok {
				p.Wait()
				return
			}

			queuedJob := job // capture for closure

			p.Go(func() {
				if ctx.Err() != nil {
					return
				}

				plan, err := q.planJob(queuedJob)
				if err != nil {
					select {
					case results <- Result{ID: queuedJob.ID, Error: err}:
					case <-ctx.Done():
					}
					return
				}
				// Notify when work actually starts (not when queued)
				if started != nil {
					select {
					case started <- JobStarted{ID: queuedJob.ID, Backend: plan.selected.name, Model: plan.selected.cfg.Model}:
					case <-ctx.Done():
					default:
					}
				}

				result := q.executeJob(ctx, queuedJob, plan)
				select {
				case results <- result:
				case <-ctx.Done():
				}
			})
		}
	}
}

// chat sends one request to a backend with a bounded timeout derived
// from parent. parent must be non-nil; the caller (chatRequest) always
// has a context from the worker pool.
func (q *BackendQueue) chat(parent context.Context, backend Backend, systemPrompt, userContent string) (string, *LLMUsage, string, *clientGeneration, error) {
	ctx, cancel := context.WithTimeout(parent, DefaultRequestTimeout)
	defer cancel()
	if err := q.oc.acquireBackend(ctx, backend.Name); err != nil {
		return "", nil, "", nil, err
	}
	defer q.oc.releaseBackend(backend.Name)
	if !q.oc.CircuitBreaker.IsHealthy(backend.Name) {
		return "", nil, "", nil, ErrBackendUnavailable
	}

	// Use custom executor if set (for testing)
	if q.executor != nil {
		content, err := q.executor.ExecuteChat(ctx, backend.Name, systemPrompt, userContent)
		return content, nil, "", nil, err // test executor doesn't return usage
	}

	cfg := q.configs[backend.Name]

	lease, err := q.oc.leaseClientAfterAdmission(backend.Name)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("no client available for backend %q: %w", backend.Name, err)
	}
	defer lease.Release()
	content, usage, _, _, err := ExecuteChatRequest(ctx, lease.Client(), cfg, systemPrompt, userContent, DefaultChatConfig)
	return content, usage, lease.keyForRotation(), lease.generationForRotation(), err
}

func (q *BackendQueue) planJob(job Job) (chatExecutionPlan, error) {
	return q.coordinator().plan(job.SystemPrompt, job.UserContent, true)
}

func (q *BackendQueue) coordinator() executionCoordinator {
	return newExecutionCoordinator(q.oc, q.configs, &q.counter)
}

func (q *BackendQueue) executeChatAttempt(ctx context.Context, info providerInfo, systemPrompt, userContent string) (chatRequestResult, error) {
	return q.chatRequestWithKeyRotation(ctx, info.name, info.cfg, systemPrompt, userContent)
}

func (q *BackendQueue) chatRequestWithKeyRotation(ctx context.Context, backendName string, cfg Config, systemPrompt, userContent string) (chatRequestResult, error) {
	return executeChatAttemptWithKeyRotation(ctx, backendName, cfg, systemPrompt, userContent, q.oc.rotateKeyForResult, q.chatRequest)
}

func (q *BackendQueue) chatRequest(ctx context.Context, backendName string, cfg Config, systemPrompt, userContent string) (chatRequestResult, error) {
	start := time.Now()
	content, usage, key, generation, err := q.chat(ctx, Backend{Name: backendName, Model: cfg.Model}, systemPrompt, userContent)
	result, err := finalizeChatRequest(backendName, cfg, start, content, usage, "", nil, err)
	result.clientKey = key
	result.clientGeneration = generation
	return result, err
}

// WorkerCount returns total number of parallel workers across all backends
func (q *BackendQueue) WorkerCount() int {
	total := 0
	for _, b := range q.oc.Backends {
		total += q.configs[b.Name].GetWorkers()
	}
	return total
}

// BackendNames returns configured backend names
func (q *BackendQueue) BackendNames() []string {
	names := make([]string, len(q.oc.Backends))
	for i, b := range q.oc.Backends {
		names[i] = b.Name
	}
	return names
}

// Shutdown is a no-op since BackendQueue uses shared OpenAIClients
// The owner (OpenAIProvider) is responsible for cleanup
func (q *BackendQueue) Shutdown() {
	// No-op: OpenAIClients are shared and owned by OpenAIProvider
}

// GetCircuitBreaker returns the circuit breaker for health monitoring
func (q *BackendQueue) GetCircuitBreaker() *CircuitBreaker {
	return q.oc.CircuitBreaker
}
