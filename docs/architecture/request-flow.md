# Request Flow

Detailed tracing of code paths from HTTP request to response in the Llambo LLM gateway service.

## Overview

Llambo processes requests through two distinct pathways:

1. **Single Request Flow** - Optimized for reliability with failover
2. **Parallel Job Flow** - Optimized for throughput with streaming results

Both paths share common provider infrastructure (circuit breaker, key rotation, error classification) but differ in routing and concurrency models.

## HTTP Entry Points

**File**: `internal/gateway/server.go`

### Route Registration
```go
// Server.SetupRoutes() - server.go:86-94
func (s *Server) SetupRoutes(mux *http.ServeMux) {
    mux.HandleFunc("/v1/chat/completions", s.handleChatCompletion)
    mux.HandleFunc("/v1/embeddings", s.handleEmbeddings)
    mux.HandleFunc("/v1/jobs", s.handleCreateJob)
    mux.HandleFunc("/v1/jobs/", s.handleGetJob)
    mux.HandleFunc("/health", s.handleHealth)
    // ... other endpoints
}
```

**Key Function**: `Server.Start()` initializes the HTTP server with timeout configuration.

## Single Request Flow (Chat Completion)

### Flow Diagram
```
HTTP POST /v1/chat/completions
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ handleChatCompletion() - handlers.go:15-80                      │
│   1. Parse & validate request body                              │
│   2. Extract system/user prompts                                │
│   3. Create scoped timeout context (120s default)               │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ s.provider.ChatWithInfoContext() - openai_provider.go:99-107    │
│   1. Collect enabled providers                                  │
│   2. Select provider (weighted round-robin)                     │
│   3. Execute with failover                                      │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ tryWithFallback() - openai_provider.go:140-201                  │
│   1. Attempt selected provider                                  │
│   2. On failure, record error                                   │
│   3. Try all other enabled providers in order                   │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ chatRequestWithKeyRotation() - openai_provider.go:204-217       │
│   1. Check API key availability                                │
│   2. Execute chat request                                      │
│   3. On HTTP 429, rotate to next key                           │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ ExecuteChatRequest() - chat_request.go:31-71                    │
│   1. Build OpenAI SDK parameters                               │
│   2. Set custom API path if configured                         │
│   3. Execute with retry logic (2 attempts)                     │
│   4. Extract content and usage                                 │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ Circuit Breaker Update                                          │
│   • RecordSuccess() - circuit_breaker.go:70-86                  │
│   • RecordFailure() - circuit_breaker.go:45-68                 │
│   • Rate limit errors → 5min cooldown                          │
│   • 3+ failures → 60s cooldown                                 │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ Response Building - handlers.go:64-78                           │
│   1. Build OpenAI-compatible response                          │
│   2. Add x_llambo metadata (provider, model, duration)         │
│   3. Return HTTP 200 with JSON                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Detailed Code Path

#### 1. Handler Entry
```go
// handlers.go:15-80
func (s *Server) handleChatCompletion(w http.ResponseWriter, r *http.Request) {
    // Parse request
    var req ChatCompletionRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "invalid request")
        return
    }

    // Extract prompts
    systemPrompt, userPrompt := extractPrompts(req.Messages)

    // Create timeout context
    ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
    defer cancel()

    // Call provider
    result, err := s.provider.ChatWithInfoContext(ctx, systemPrompt, userPrompt)
    // ... handle response
}
```

#### 2. Provider Selection & Failover
```go
// openai_provider.go:99-107
func (o *OpenAIProvider) ChatWithInfoContext(ctx context.Context, system, user string) (ChatResult, error) {
    // Collect enabled providers
    enabled := o.clients.collectEnabledProviders()
    if len(enabled) == 0 {
        return ChatResult{}, ErrNoProviders
    }

    // Weighted selection (by worker count)
    selected := o.selectWeightedProvider(enabled)

    // Execute with failover
    return o.tryWithFallback(ctx, selected, enabled, system, user)
}
```

#### 3. Failover Logic
```go
// openai_provider.go:140-201
func (o *OpenAIProvider) tryWithFallback(ctx context.Context, first string, all []string, system, user string) (ChatResult, error) {
    var lastErr error

    // Try selected provider first
    result, err := o.chatRequestWithKeyRotation(ctx, first, system, user)
    if err == nil {
        return result, nil
    }

    // Record failure
    o.clients.circuitBreaker.RecordFailure(first, err)
    lastErr = err

    // Try all other providers
    for _, provider := range all {
        if provider == first {
            continue
        }

        result, err := o.chatRequestWithKeyRotation(ctx, provider, system, user)
        if err == nil {
            return result, nil
        }

        o.clients.circuitBreaker.RecordFailure(provider, err)
        lastErr = err
    }

    return ChatResult{}, fmt.Errorf("all providers failed: %w", lastErr)
}
```

#### 4. Key Rotation
```go
// openai_provider.go:204-217
func (o *OpenAIProvider) chatRequestWithKeyRotation(ctx context.Context, backendName, system, user string) (ChatResult, error) {
    cfg := o.clients.configs[backendName]

    // Check if backend has available keys
    if !o.clients.keyRotator.HasAvailableKeys(backendName) {
        return ChatResult{}, fmt.Errorf("no available keys for %s", backendName)
    }

    result, err := o.clients.ExecuteChatRequest(ctx, backendName, system, user)

    // Handle rate limits
    if err != nil && IsRateLimitError(err) {
        o.clients.keyRotator.MarkRateLimited(backendName)
    }

    return result, err
}
```

#### 5. Request Execution
```go
// chat_request.go:31-71
func (c *OpenAIClients) ExecuteChatRequest(ctx context.Context, backendName, system, user string) (ChatResult, error) {
    client := c.clients[backendName]
    cfg := c.configs[backendName]

    // Build request
    req := openai.ChatCompletionRequest{
        Model: cfg.Model,
        Messages: []openai.ChatCompletionMessage{
            {Role: "system", Content: system},
            {Role: "user", Content: user},
        },
    }

    // Execute with retry
    var resp openai.ChatCompletionResponse
    var err error

    for attempt := 0; attempt < 2; attempt++ {
        resp, err = client.Chat.Completions.New(ctx, req)
        if err == nil {
            break
        }

        if !IsRetryable(err) {
            break
        }

        time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
    }

    // Extract result
    return ChatResult{
        Content: resp.Choices[0].Message.Content,
        Usage:   resp.Usage,
    }, err
}
```

### Failover Decision Points (Single Request)

1. **Provider Selection Point** - `openai_provider.go:125-137`
   - Weighted round-robin based on worker count
   - Only healthy providers considered (circuit breaker)

2. **Key Rotation Point** - `openai_provider.go:211-214`
   - HTTP 429 triggers key rotation
   - All keys exhausted → circuit breaker

3. **Circuit Breaker Thresholds** - `circuit_breaker.go:45-68`
   - 3 consecutive failures → 60s disable
   - Rate limit/quota error → 5min disable

4. **Sequential Failover** - `openai_provider.go:169-198`
   - Primary fails → try all others in order
   - All fail → aggregate error

## Parallel Job Flow (Batch Processing)

### Flow Diagram
```
HTTP POST /v1/jobs
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ handleCreateJob() - handlers.go:151-171                         │
│   1. Parse job request                                         │
│   2. Create job in JobManager                                  │
│   3. Start background processing                               │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ s.jobManager.CreateJob() - jobs.go:89-112                       │
│   1. Generate unique job ID                                    │
│   2. Create Job struct with status tracking                    │
│   3. Convert requests to provider.Job format                   │
│   4. Start processJob() in goroutine                           │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ processJob() - jobs.go:230-249                                  │
│   1. Create job and result channels                            │
│   2. Convert requests to queue.Job format                      │
│   3. Submit to BackendQueue                                    │
│   4. Collect results                                           │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ s.queue.ProcessStream() - jobs.go:162-174                      │
│   (wraps BackendQueue.ProcessStream())                         │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ BackendQueue.ProcessStream() - queue.go:127-152                 │
│   1. Create worker pool                                        │
│   2. For each job in channel:                                  │
│      a. Select healthy backend (round-robin)                   │
│      b. Submit to goroutine pool                               │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ q.executeJob() - queue.go:87-107                               │
│   1. Get backend config                                        │
│   2. Choose execution path:                                    │
│      • Native Gemini API (queue.go:167-178)                    │
│      • OpenAI-compatible (queue.go:182)                        │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ ExecuteChatRequest() - chat_request.go:31-71                   │
│   (Same as single request path)                                │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ Results Channel → collectResults() - jobs.go:250-265            │
│   1. Receive result from queue                                 │
│   2. Lock job mutex                                            │
│   3. Update counts and append result                           │
│   4. Update status if all done                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Detailed Code Path

#### 1. Job Creation
```go
// jobs.go:89-112
func (jm *JobManager) CreateJob(requests []JobRequest) (*Job, error) {
    jobID := generateJobID()

    job := &Job{
        ID:            jobID,
        Status:        "pending",
        TotalRequests: len(requests),
        Results:       make([]JobResult, 0, len(requests)),
    }

    jm.mu.Lock()
    jm.jobs[jobID] = job
    jm.mu.Unlock()

    // Start processing in background
    go jm.processJob(job, requests)

    return job, nil
}
```

#### 2. Backend Selection (Round-Robin)
```go
// queue.go:74-84
func (q *BackendQueue) nextBackend() string {
    q.counter.Add(1)
    idx := q.counter.Load() % uint64(len(q.backends))

    // Skip unhealthy backends
    for i := 0; i < len(q.backends); i++ {
        backend := q.backends[(idx+uint64(i))%uint64(len(q.backends))]
        if q.clients.circuitBreaker.IsHealthy(backend.Name) {
            return backend.Name
        }
    }

    return "" // No healthy backends
}
```

#### 3. Parallel Execution
```go
// queue.go:127-152
func (q *BackendQueue) ProcessStream(ctx context.Context, jobs <-chan providers.Job, progress func(providers.Result)) {
    totalWorkers := 0
    for _, backend := range q.backends {
        totalWorkers += backend.Workers
    }

    pool := pool.NewWithResults[providers.Result]()
    pool.WithMaxGoroutines(totalWorkers)

    for job := range jobs {
        job := job // Capture for closure

        pool.Go(func() providers.Result {
            backendName := q.nextBackend()
            if backendName == "" {
                return providers.Result{
                    ID:      job.ID,
                    Error:   "no healthy backends available",
                    Success: false,
                }
            }

            return q.executeJob(ctx, backendName, job)
        })
    }

    // Stream results
    for _, result := range pool.Wait() {
        progress(result)
    }
}
```

### Failover Decision Points (Parallel Jobs)

1. **Backend Selection Point** - `queue.go:74-84`
   - Round-robin across healthy backends only
   - Skips disabled backends (circuit breaker)

2. **Worker Pool Limits** - `queue.go:186-192`
   - Total workers = sum of all backend workers
   - Prevents overloading any single backend

3. **Shared Circuit Breaker** - `client_factory.go:12-20`
   - Single health state shared between single requests and jobs
   - Job failures affect single request routing and vice versa

## Shared Infrastructure

### Circuit Breaker State Machine
```
                    ┌──────────────┐
                    │   HEALTHY    │
                    │ failures = 0 │
                    └──────┬───────┘
                           │
              Success      │      Failure
           ┌───────────────┼────────────────┐
           │               │                │
           ▼               ▼                ▼
    ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
    │   HEALTHY    │ │   DEGRADED   │ │  RATE_LIMIT  │
    │ failures = 0 │ │ failures < 3 │ │   detected   │
    └──────────────┘ └──────┬───────┘ └──────┬───────┘
                           │                │
                   3rd failure              │
                           │                │
                           ▼                ▼
                    ┌──────────────────────────────┐
                    │          DISABLED            │
                    │  disabled_until = now + X    │
                    │  (60s failures, 5m rate)     │
                    └──────────────┬───────────────┘
                                   │
                       Time passes, cooldown expires
                                   │
                                   ▼
                    ┌──────────────────────────────┐
                    │     RECOVERY (implicit)      │
                    │  Next request is attempted   │
                    │  Success → HEALTHY           │
                    │  Failure → DISABLED again    │
                    └──────────────────────────────┘
```

**Key Functions**:
- `RecordSuccess()` - `circuit_breaker.go:70-86`
- `RecordFailure()` - `circuit_breaker.go:45-68`
- `IsHealthy()` - `circuit_breaker.go:94-105`

### Key Rotation System

**File**: `providers/key_rotator.go`

```go
// Key rotation flow
1. Multiple API keys configured in config.json
2. HTTP 429 → MarkRateLimited() rotates to next key
3. All keys exhausted → circuit breaker disables backend
4. 5-minute cooldown for rate-limited keys
5. Auto-reset after cooldown expires
```

**Key Functions**:
- `MarkRateLimited()` - `key_rotator.go:32-45`
- `HasAvailableKeys()` - `key_rotator.go:47-55`
- `RotateKey()` - `openai_provider.go:211-214`

### Error Classification

**File**: `providers/errors_classification.go`

```go
// Classify error type
func ClassifyError(err error) ErrorType {
    if IsRateLimitError(err) {
        return ErrorTypeRateLimit
    }
    if IsRetryable(err) {
        return ErrorTypeRetryable
    }
    return ErrorTypePermanent
}
```

**Decision Points**:
- Rate limit errors trigger key rotation
- Retryable errors get 2 retry attempts
- Permanent errors fail immediately

## Configuration-Driven Behavior

All provider behavior configured via `~/.config/llambo/config.json`:

```json
{
  "providers": {
    "openai": {
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "api_keys": ["sk-key1", "sk-key2", "sk-key3"],
      "workers": 3,
      "priority": 1,
      "enabled": true
    }
  }
}
```

**Config Fields Affecting Flow**:
- `workers`: Weight for provider selection, worker pool size
- `api_keys`: Key rotation pool size
- `priority`: Load balancing order (single requests)
- `enabled`: Whether backend participates in routing

## Performance Characteristics

### Single Request Path
- **Latency**: Adds ~5ms overhead for provider selection
- **Reliability**: Sequential failover across all providers
- **Concurrency**: Limited by HTTP server worker pool

### Parallel Job Path
- **Throughput**: Scales with total workers across all backends
- **Latency**: First results stream immediately
- **Resource**: Goroutine pool limits concurrent requests

### Shared Limits
- **Circuit Breaker**: Prevents cascading failures
- **Key Rotation**: Extends rate limits 3x (with 3 keys)
- **Error Classification**: Smart retry logic

## Debugging & Monitoring

### Health Endpoint
```
GET /health
{
  "status": "healthy",
  "providers": {
    "openai": {
      "healthy": true,
      "workers_available": 3,
      "keys_available": 2
    }
  }
}
```

### Response Headers
- `X-Llambo-Provider`: Backend that served request
- `X-Llambo-Model`: Model used for completion
- `X-Llambo-Duration`: Request processing time

### Job Status Endpoint
```
GET /v1/jobs/{id}
{
  "id": "job-abc123",
  "status": "processing",
  "completed": 5,
  "total": 10,
  "results": [...]
}
```

## Common Flow Variations

### Embeddings Flow
Similar to single request flow but uses:
- `handleEmbeddings()` - `handlers.go:82-114`
- `ExecuteEmbeddingRequest()` - `embeddings.go:45-85`

### Model Listing Flow
- `handleModels()` - `handlers.go:116-134`
- Returns aggregated models from all enabled providers

## Critical Code References

| Component | File | Key Functions |
|-----------|------|---------------|
| HTTP Server | `internal/gateway/server.go` | `Server.Start()`, `SetupRoutes()` |
| Request Handlers | `internal/gateway/handlers.go` | `handleChatCompletion()`, `handleCreateJob()` |
| Job Management | `internal/gateway/jobs.go` | `CreateJob()`, `processJob()` |
| Single Request Provider | `providers/openai_provider.go` | `ChatWithInfoContext()`, `tryWithFallback()` |
| Parallel Queue | `providers/queue.go` | `ProcessStream()`, `nextBackend()` |
| Circuit Breaker | `providers/circuit_breaker.go` | `RecordSuccess()`, `RecordFailure()` |
| Request Execution | `providers/chat_request.go` | `ExecuteChatRequest()` |
| Key Rotation | `providers/key_rotator.go` | `MarkRateLimited()`, `HasAvailableKeys()` |
| Client Factory | `providers/client_factory.go` | `NewOpenAIClients()` |

## Flow Summary

**Single Request**: Reliability-focused with sequential failover
```
HTTP → Handler → Provider Selection → Key Rotation → API → Response
        ↑          ↑                    ↑           ↑
        │          │                    │           │
      Validation  Failover          429 Handling  Metadata
```

**Parallel Job**: Throughput-focused with streaming results
```
HTTP → Handler → Job Manager → Backend Queue → Worker Pool → API
        ↑          ↑             ↑              ↑           ↑
        │          │             │              │           │
      Validation  Status       Round-Robin   Concurrency  Circuit
      Tracking               Load Balancing    Limits     Breaker
```

Both paths converge at the shared provider infrastructure, ensuring consistent health tracking, error handling, and configuration behavior across all request types.