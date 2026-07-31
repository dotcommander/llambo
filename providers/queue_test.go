package providers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mockExecutor implements ChatExecutor for testing
type mockExecutor struct {
	responses map[string]string // backendName -> response
	errors    map[string]error  // backendName -> error
	calls     []string          // track call order
	mu        sync.Mutex
	delay     time.Duration // optional delay to simulate work
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
}

func (m *mockExecutor) ExecuteChat(ctx context.Context, backendName string, systemPrompt, userContent string) (string, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(m.delay):
		}
	}

	m.mu.Lock()
	m.calls = append(m.calls, backendName)
	m.mu.Unlock()

	if err, ok := m.errors[backendName]; ok {
		return "", err
	}
	return m.responses[backendName], nil
}

// createTestQueue creates a BackendQueue with mock backends for testing
func createTestQueue(backendNames []string, workersPerBackend int) (*BackendQueue, *mockExecutor) {
	configs := make(map[string]Config)
	backends := make([]Backend, len(backendNames))

	for i, name := range backendNames {
		configs[name] = Config{
			Model:   "test-model",
			Workers: workersPerBackend,
			Enabled: true,
		}
		backends[i] = Backend{
			Name:  name,
			Model: "test-model",
		}
	}

	oc := &OpenAIClients{
		Clients:        nil, // not needed with mock executor
		Backends:       backends,
		CircuitBreaker: NewCircuitBreaker(backendNames, nil),
		CostTracker:    NewCostTracker(backendNames),
	}

	queue := NewBackendQueue(oc, configs)
	executor := newMockExecutor()
	queue.SetExecutor(executor)

	return queue, executor
}

func TestBackendQueue_Process_AllSuccess(t *testing.T) {
	t.Parallel()
	queue, executor := createTestQueue([]string{"backend1", "backend2"}, 2)

	// Set up responses for all backends
	executor.responses["backend1"] = "response from backend1"
	executor.responses["backend2"] = "response from backend2"

	jobs := []Job{
		{ID: "job1", SystemPrompt: "sys", UserContent: "content1"},
		{ID: "job2", SystemPrompt: "sys", UserContent: "content2"},
		{ID: "job3", SystemPrompt: "sys", UserContent: "content3"},
	}

	results := queue.Process(context.Background(), jobs)

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// All results should succeed
	for _, r := range results {
		if r.Error != nil {
			t.Errorf("job %s failed unexpectedly: %v", r.ID, r.Error)
		}
		if r.Content == "" {
			t.Errorf("job %s has empty content", r.ID)
		}
	}
}

func TestBackendQueue_ExecuteJob_FallsBackToHealthyProvider(t *testing.T) {
	t.Parallel()
	queue, executor := createTestQueue([]string{"good", "bad"}, 2)

	executor.responses["good"] = "success response"
	executor.errors["bad"] = errors.New("backend failure")

	plan := chatExecutionPlan{
		selected: providerInfo{name: "bad", cfg: queue.configs["bad"], weight: 2},
		enabled: []providerInfo{
			{name: "bad", cfg: queue.configs["bad"], weight: 2},
			{name: "good", cfg: queue.configs["good"], weight: 2},
		},
		plannedProvider: "bad",
		intent:          IntentChat,
		estimatedTokens: 1,
	}

	result := queue.executeJob(context.Background(), Job{ID: "job1", SystemPrompt: "sys", UserContent: "content1"}, plan)
	if result.Error != nil {
		t.Fatalf("expected fallback success, got error: %v", result.Error)
	}
	if result.Backend != "good" {
		t.Fatalf("expected final backend good, got %q", result.Backend)
	}
	if result.Content != "success response" {
		t.Fatalf("expected fallback content, got %q", result.Content)
	}

	executor.mu.Lock()
	defer executor.mu.Unlock()
	if len(executor.calls) != 2 || executor.calls[0] != "bad" || executor.calls[1] != "good" {
		t.Fatalf("expected failover call order [bad good], got %+v", executor.calls)
	}
}

func TestBackendQueue_ProcessStream_StreamsResults(t *testing.T) {
	t.Parallel()
	queue, executor := createTestQueue([]string{"backend1", "backend2"}, 2)

	executor.responses["backend1"] = "response1"
	executor.responses["backend2"] = "response2"
	executor.delay = 10 * time.Millisecond // small delay to simulate work

	jobs := make(chan Job, 3)
	results := make(chan Result, 3)

	// Send jobs
	jobs <- Job{ID: "job1", SystemPrompt: "sys", UserContent: "content1"}
	jobs <- Job{ID: "job2", SystemPrompt: "sys", UserContent: "content2"}
	jobs <- Job{ID: "job3", SystemPrompt: "sys", UserContent: "content3"}
	close(jobs)

	// Process in goroutine
	go queue.ProcessStream(context.Background(), jobs, results, 0, nil)

	// Collect results with timeout
	var collected []Result
	timeout := time.After(5 * time.Second)

	for i := 0; i < 3; i++ {
		select {
		case r := <-results:
			collected = append(collected, r)
		case <-timeout:
			t.Fatalf("timeout waiting for result %d", i+1)
		}
	}

	if len(collected) != 3 {
		t.Errorf("expected 3 results, got %d", len(collected))
	}

	// Verify all jobs completed
	seenIDs := make(map[string]bool)
	for _, r := range collected {
		seenIDs[r.ID] = true
		if r.Error != nil {
			t.Errorf("job %s failed: %v", r.ID, r.Error)
		}
	}

	for _, id := range []string{"job1", "job2", "job3"} {
		if !seenIDs[id] {
			t.Errorf("missing result for job %s", id)
		}
	}
}

func TestBackendQueue_ProcessStream_NotifiesStarted(t *testing.T) {
	t.Parallel()
	queue, executor := createTestQueue([]string{"backend1"}, 2)

	executor.responses["backend1"] = "response"
	executor.delay = 50 * time.Millisecond

	jobs := make(chan Job, 2)
	results := make(chan Result, 2)
	started := make(chan JobStarted, 2)

	jobs <- Job{ID: "job1", SystemPrompt: "sys", UserContent: "content1"}
	jobs <- Job{ID: "job2", SystemPrompt: "sys", UserContent: "content2"}
	close(jobs)

	done := make(chan struct{})
	go func() {
		defer close(done)
		queue.ProcessStream(context.Background(), jobs, results, 0, started)
	}()

	// Range-loop on started: ProcessStream is the sole closer (see channel
	// ownership comment in queue.go). The loop exits when started closes
	// after all workers finish.
	var startedJobs []JobStarted
	for s := range started {
		startedJobs = append(startedJobs, s)
	}

	// Drain results
	for i := 0; i < 2; i++ {
		select {
		case <-results:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for results")
		}
	}

	// Confirm ProcessStream returned.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ProcessStream did not return")
	}

	if len(startedJobs) != 2 {
		t.Errorf("expected 2 started notifications, got %d", len(startedJobs))
	}

	// Verify started notifications have backend info
	for _, s := range startedJobs {
		if s.Backend == "" {
			t.Error("started notification missing backend")
		}
		if s.Model == "" {
			t.Error("started notification missing model")
		}
	}
}

func TestBackendQueue_NextBackend_RoundRobin(t *testing.T) {
	t.Parallel()
	queue, _ := createTestQueue([]string{"b1", "b2", "b3"}, 1)

	// Call nextBackend multiple times and verify round-robin distribution
	backendCounts := make(map[string]int)
	for i := 0; i < 9; i++ {
		backend := queue.nextBackend()
		backendCounts[backend.Name]++
	}

	// Each backend should be selected exactly 3 times
	for _, name := range []string{"b1", "b2", "b3"} {
		if backendCounts[name] != 3 {
			t.Errorf("expected backend %s to be selected 3 times, got %d", name, backendCounts[name])
		}
	}
}

func TestBackendQueue_NextBackend_SkipsUnhealthy(t *testing.T) {
	t.Parallel()
	queue, _ := createTestQueue([]string{"healthy1", "unhealthy", "healthy2"}, 1)

	// Mark one backend as unhealthy
	queue.oc.CircuitBreaker.RecordFailure("unhealthy", errors.New("rate limit"))

	// Call nextBackend multiple times
	backendCounts := make(map[string]int)
	for i := 0; i < 10; i++ {
		backend := queue.nextBackend()
		backendCounts[backend.Name]++
	}

	// Unhealthy backend should not be selected
	if backendCounts["unhealthy"] > 0 {
		t.Errorf("unhealthy backend was selected %d times, expected 0", backendCounts["unhealthy"])
	}

	// Healthy backends should share the load
	if backendCounts["healthy1"] == 0 {
		t.Error("healthy1 was never selected")
	}
	if backendCounts["healthy2"] == 0 {
		t.Error("healthy2 was never selected")
	}
}

func TestBackendQueue_NextBackend_AllUnhealthy_FallsBack(t *testing.T) {
	t.Parallel()
	queue, _ := createTestQueue([]string{"b1", "b2"}, 1)

	// Mark all backends as unhealthy
	queue.oc.CircuitBreaker.RecordFailure("b1", errors.New("rate limit"))
	queue.oc.CircuitBreaker.RecordFailure("b2", errors.New("rate limit"))

	// Should still return a backend (fallback behavior)
	backend := queue.nextBackend()
	if backend.Name == "" {
		t.Error("nextBackend returned empty backend when all unhealthy")
	}
}

func TestBackendQueue_WorkerCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		backends []string
		workers  int
		expected int
	}{
		{"single backend", []string{"b1"}, 3, 3},
		{"multiple backends same workers", []string{"b1", "b2"}, 2, 4},
		{"three backends", []string{"b1", "b2", "b3"}, 3, 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			queue, _ := createTestQueue(tt.backends, tt.workers)
			got := queue.WorkerCount()
			if got != tt.expected {
				t.Errorf("WorkerCount() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestBackendQueue_WorkerCount_MixedWorkers(t *testing.T) {
	t.Parallel()
	// Create queue with different worker counts per backend
	configs := map[string]Config{
		"b1": {Model: "m1", Workers: 2, Enabled: true},
		"b2": {Model: "m2", Workers: 5, Enabled: true},
		"b3": {Model: "m3", Workers: 3, Enabled: true},
	}

	backends := []Backend{
		{Name: "b1", Model: "m1"},
		{Name: "b2", Model: "m2"},
		{Name: "b3", Model: "m3"},
	}

	oc := &OpenAIClients{
		Backends:       backends,
		CircuitBreaker: NewCircuitBreaker([]string{"b1", "b2", "b3"}, nil),
		CostTracker:    NewCostTracker([]string{"b1", "b2", "b3"}),
	}

	queue := NewBackendQueue(oc, configs)

	expected := 2 + 5 + 3 // 10
	got := queue.WorkerCount()
	if got != expected {
		t.Errorf("WorkerCount() = %d, want %d", got, expected)
	}
}

func TestBackendQueue_BackendNames(t *testing.T) {
	t.Parallel()
	queue, _ := createTestQueue([]string{"alpha", "beta", "gamma"}, 1)

	names := queue.BackendNames()

	if len(names) != 3 {
		t.Errorf("expected 3 backend names, got %d", len(names))
	}

	expected := map[string]bool{"alpha": true, "beta": true, "gamma": true}
	for _, name := range names {
		if !expected[name] {
			t.Errorf("unexpected backend name: %s", name)
		}
	}
}

func TestBackendQueue_GetCircuitBreaker(t *testing.T) {
	t.Parallel()
	queue, _ := createTestQueue([]string{"b1"}, 1)

	cb := queue.GetCircuitBreaker()
	if cb == nil {
		t.Error("GetCircuitBreaker returned nil")
	}

	// Verify it's the same circuit breaker
	if cb != queue.oc.CircuitBreaker {
		t.Error("GetCircuitBreaker returned different instance")
	}
}

func TestBackendQueue_Shutdown(t *testing.T) {
	t.Parallel()
	queue, _ := createTestQueue([]string{"b1"}, 1)

	// Shutdown should not panic
	queue.Shutdown()
}

func TestBackendQueue_Process_RecordsCircuitBreakerHealth(t *testing.T) {
	t.Parallel()
	queue, executor := createTestQueue([]string{"good", "bad"}, 1)

	executor.responses["good"] = "success"
	executor.errors["bad"] = errors.New("failure")

	// Reset counter to ensure predictable ordering
	queue.counter = 0

	// Process jobs - first goes to good, second to bad
	jobs := []Job{
		{ID: "job1", SystemPrompt: "sys", UserContent: "content1"},
		{ID: "job2", SystemPrompt: "sys", UserContent: "content2"},
	}

	queue.Process(context.Background(), jobs)

	// Check circuit breaker recorded the failure
	badHealth := queue.oc.CircuitBreaker.GetHealth("bad")
	if badHealth.Failures == 0 {
		t.Error("expected failure to be recorded for bad backend")
	}
}

func TestBackendQueue_Process_Empty(t *testing.T) {
	t.Parallel()
	queue, _ := createTestQueue([]string{"b1"}, 1)

	results := queue.Process(context.Background(), nil)

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestBackendQueue_ProcessStream_LimitsMaxWorkers(t *testing.T) {
	t.Parallel()
	// Create queue with high worker count
	queue, executor := createTestQueue([]string{"b1", "b2"}, 10)

	executor.responses["b1"] = "response1"
	executor.responses["b2"] = "response2"

	jobs := make(chan Job, 5)
	results := make(chan Result, 5)

	for i := 0; i < 5; i++ {
		jobs <- Job{ID: string(rune('a' + i)), SystemPrompt: "sys", UserContent: "content"}
	}
	close(jobs)

	// Limit to 2 workers
	go queue.ProcessStream(context.Background(), jobs, results, 2, nil)

	// Collect results
	var collected int
	timeout := time.After(5 * time.Second)
	for collected < 5 {
		select {
		case <-results:
			collected++
		case <-timeout:
			t.Fatalf("timeout after collecting %d results", collected)
		}
	}

	if collected != 5 {
		t.Errorf("expected 5 results, got %d", collected)
	}
}
