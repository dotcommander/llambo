# Architecture

Deep dive into Llambo's internal architecture.

## Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                              CLI (cmd/)                               │
│  root.go → serve.go → config.go                                      │
└───────────────────────────────┬──────────────────────────────────────┘
                                │
┌───────────────────────────────▼──────────────────────────────────────┐
│                         Gateway (internal/gateway/)                   │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐ │
│  │  server.go  │  │ handlers.go │  │   jobs.go   │  │  types.go   │ │
│  │   (setup)   │  │ (endpoints) │  │ (job mgmt)  │  │  (OpenAI)   │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘ │
└───────────────────────────────┬──────────────────────────────────────┘
                                │
┌───────────────────────────────▼──────────────────────────────────────┐
│                         Providers (providers/)                        │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                     OpenAIClients                                │ │
│  │  • Per-backend OpenAI SDK clients                               │ │
│  │  • Shared CircuitBreaker instance                               │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│  ┌──────────────────────┐  ┌──────────────────────────────────────┐ │
│  │   OpenAIProvider     │  │          BackendQueue                │ │
│  │  (single requests)   │  │     (parallel job processing)        │ │
│  │  • Round-robin       │  │  • Worker pools per backend          │ │
│  │  • Automatic retry   │  │  • Streaming results                 │ │
│  │  • Failover          │  │  • Job distribution                  │ │
│  └──────────────────────┘  └──────────────────────────────────────┘ │
│  ┌──────────────────────┐  ┌──────────────────────────────────────┐ │
│  │   CircuitBreaker     │  │        Error Handling                │ │
│  │  • Per-backend state │  │  • RateLimitError detection          │ │
│  │  • Cooldown timers   │  │  • RetryableError classification     │ │
│  │  • Auto-recovery     │  │  • Error wrapping                    │ │
│  └──────────────────────┘  └──────────────────────────────────────┘ │
└───────────────────────────────┬──────────────────────────────────────┘
                                │
┌───────────────────────────────▼──────────────────────────────────────┐
│                         OpenAI Go SDK                                 │
│  • Provider abstraction (OpenAI, OpenRouter, Gemini, etc.)           │
│  • Request execution                                                  │
│  • Response parsing                                                   │
└──────────────────────────────────────────────────────────────────────┘
```

## Component Details

### CLI Layer (`cmd/`)

**root.go** - Cobra root command setup
```go
var rootCmd = &cobra.Command{
    Use:   "llambo",
    Short: "High-performance LLM gateway",
}
```

**serve.go** - Server startup with graceful shutdown
```go
// Handles SIGINT/SIGTERM
// Configures HTTP timeouts
// Initializes providers from config
```

**config.go** - Configuration management
```go
// llambo config init - creates default config
// llambo config show - displays current config
```

### Gateway Layer (`internal/gateway/`)

**server.go** - HTTP server setup
```go
type Server struct {
    provider     providers.Provider
    jobManager   *JobManager
    backendQueue *providers.BackendQueue
}

func (s *Server) SetupRoutes(mux *http.ServeMux) {
    mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
    mux.HandleFunc("/v1/embeddings", s.handleEmbeddings)
    mux.HandleFunc("/v1/models", s.handleModels)
    mux.HandleFunc("/v1/jobs", s.handleJobs)
    mux.HandleFunc("/v1/jobs/", s.handleJobStatus)
    mux.HandleFunc("/health", s.handleHealth)
    mux.HandleFunc("/providers", s.handleProviders)
}
```

**handlers.go** - Request handlers
- Each endpoint has dedicated handler
- Error responses follow OpenAI format
- `x_llambo` metadata added to responses

**jobs.go** - Job lifecycle management
```go
type JobManager struct {
    jobs map[string]*Job
    mu   sync.RWMutex
}

type Job struct {
    ID            string
    Status        string  // pending, processing, completed, failed, cancelled
    TotalRequests int
    Completed     int
    Failed        int
    Results       []JobResult
    mu            sync.Mutex
}
```

**types.go** - OpenAI-compatible types
```go
type ChatCompletionRequest struct {
    Model       string    `json:"model"`
    Messages    []Message `json:"messages"`
    Temperature *float64  `json:"temperature,omitempty"`
    MaxTokens   *int      `json:"max_tokens,omitempty"`
}

type ChatCompletionResponse struct {
    ID      string   `json:"id"`
    Choices []Choice `json:"choices"`
    XLlambo *XLlambo `json:"x_llambo,omitempty"`  // Extension
}
```

### Provider Layer (`providers/`)

**config.go** - Configuration loading
```go
type Config struct {
    ProviderType   string            `json:"provider_type"`
    BaseURL        string            `json:"base_url"`
    Model          string            `json:"model"`
    Workers        int               `json:"workers"`
    Priority       int               `json:"priority"`
    Enabled        bool              `json:"enabled"`
    EnvVar         string            `json:"env_var"`
    RequiresKey    bool              `json:"requires_key"`
    ExtraHeaders   map[string]string `json:"extra_headers"`
    APIPath        string            `json:"api_path"` // legacy compatibility; serving path ignores it
}
```

**openai_provider.go** - Single request handling
```go
type OpenAIProvider struct {
    clients *OpenAIClients
}

func (o *OpenAIProvider) ChatWithInfo(ctx context.Context, system, user string) (ChatResult, error) {
    // 1. Get healthy backends (round-robin)
    // 2. Execute request with retry
    // 3. On failure, try next backend
    // 4. Update circuit breaker
    // 5. Return result with metadata
}
```

**queue.go** - Parallel job processing
```go
type BackendQueue struct {
    backends []*Backend
    clients  *OpenAIClients
    counter  atomic.Uint64  // Round-robin counter
}

func (q *BackendQueue) ProcessStream(ctx context.Context, jobs <-chan Job, progress func(Result)) {
    // 1. Pull jobs from channel
    // 2. Select backend (round-robin, prefer healthy)
    // 3. Execute in goroutine pool
    // 4. Stream results via progress callback
}
```

**circuit_breaker.go** - Health tracking
```go
type CircuitBreaker struct {
    backends map[string]*BackendHealth
    mu       sync.RWMutex
}

type BackendHealth struct {
    Failures      int
    Disabled      bool
    DisabledUntil time.Time
}

func (cb *CircuitBreaker) RecordFailure(backend string, err error) {
    // Rate limit errors → immediate disable (5 min)
    // 3+ failures → disable (60 sec)
}

func (cb *CircuitBreaker) RecordSuccess(backend string) {
    // Reset failure count
    // Re-enable if was disabled
}
```

**client_factory.go** - OpenAI client creation
```go
type OpenAIClients struct {
    clients        map[string]*openai.Client
    configs        map[string]*Config
    circuitBreaker *CircuitBreaker
}

func NewOpenAIClients(ctx context.Context, configs map[string]*Config) (*OpenAIClients, error) {
    // Create dedicated OpenAI SDK client per backend
    // Each client has its own BaseURL
    // Share single CircuitBreaker
}
```

## Request Flow

### Single Chat Completion

```
POST /v1/chat/completions
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ handleChatCompletions()                                          │
│   1. Parse request body                                          │
│   2. Extract system/user prompts                                 │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ OpenAIProvider.ChatWithInfo()                                    │
│   1. Get backends sorted by priority                             │
│   2. Filter to healthy backends (circuit breaker)                │
│   3. Round-robin select from healthy                             │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ ExecuteChatRequest()                                             │
│   1. Build OpenAI SDK request from config                        │
│   2. Set custom API path if configured                           │
│   3. Execute with timeout (120s)                                 │
│   4. Retry on transient errors (2 attempts)                      │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ Circuit Breaker Update                                           │
│   Success → RecordSuccess(backend)                               │
│   Failure → RecordFailure(backend, err)                          │
│             May trigger backend disable                          │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ Response Building                                                │
│   1. Extract content from OpenAI SDK response                    │
│   2. Build OpenAI-compatible response                            │
│   3. Add x_llambo metadata (backend, model, duration)            │
└─────────────────────────────────────────────────────────────────┘
```

### Batch Job Processing

```
POST /v1/jobs
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ JobManager.CreateJob()                                           │
│   1. Generate job ID                                             │
│   2. Create Job struct (status: pending)                         │
│   3. Convert requests to queue.Job format                        │
│   4. Start background processing                                 │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ Background: BackendQueue.ProcessStream()                         │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │ For each job in channel:                                │   │
│   │   1. Select healthy backend (round-robin)               │   │
│   │   2. Submit to goroutine pool                           │   │
│   └─────────────────────────────────────────────────────────┘   │
│                     │                                            │
│   ┌─────────────────▼─────────────────────────────────────────┐ │
│   │ Goroutine Pool (per backend workers):                     │ │
│   │   1. ExecuteChatRequest()                                 │ │
│   │   2. Update circuit breaker                               │ │
│   │   3. Send result via progress callback                    │ │
│   └───────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│ Progress Callback (per result):                                  │
│   1. Lock job mutex                                              │
│   2. Append result                                               │
│   3. Update completed/failed counts                              │
│   4. Update job status if all done                               │
│   5. Unlock mutex                                                │
└─────────────────────────────────────────────────────────────────┘
```

## Circuit Breaker State Machine

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

## Concurrency Model

### Thread Safety

- **Job state**: Protected by `sync.Mutex` per job
- **Job map**: Protected by `sync.RWMutex` in JobManager
- **Circuit breaker**: Protected by `sync.RWMutex`
- **Backend selection**: Atomic counter for round-robin

### Goroutine Pools

Using `sourcegraph/conc` for managed pools:

```go
// Parallel batch processing with results
pool := pool.NewWithResults[Result]()
pool.WithMaxGoroutines(totalWorkers)

for job := range jobs {
    pool.Go(func() Result {
        return executeJob(job)
    })
}

results := pool.Wait()
```

### Graceful Shutdown

```go
// In serve.go
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

<-quit

ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

server.Shutdown(ctx)
```

## Error Classification

```go
// Rate limit detection
func IsRateLimitError(err error) bool {
    // HTTP 429
    // "rate limit" in message
    // "quota" in message
}

// Retryable detection
func IsRetryable(err error) bool {
    // Connection errors
    // Timeout errors
    // 5xx status codes
    // NOT: 4xx, parse errors, context cancelled
}
```

## Configuration Loading

```go
func LoadConfigs() (map[string]*Config, error) {
    // 1. Read ~/.config/llambo/config.json
    // 2. Parse JSON into map[string]*Config
    // 3. Apply defaults (workers=2, provider_type="openai")
    // 4. Resolve API keys from environment
    // 5. Return validated configs
}
```

## Key Design Decisions

### Per-Backend OpenAI Clients

Each backend needs its own OpenAI SDK client because:
- BaseURL varies per provider
- Headers vary per provider
- API path may vary per provider

Sharing would cause routing issues.

### Shared Circuit Breaker

Single CircuitBreaker across OpenAIProvider and BackendQueue:
- Consistent health view
- Single request failures affect batch routing
- Batch failures affect single request routing

### Round-Robin with Priority

Not pure round-robin:
1. Sort backends by priority
2. Filter to healthy only
3. Round-robin within same-priority tier
4. Fall back to lower priority when higher exhausted

### Streaming Results

Jobs don't wait for all requests:
- Results added as they complete
- Client can poll and get partial results
- Progress visible immediately

### Automatic Cleanup

Old jobs cleaned up automatically:
- Background goroutine every 5 minutes
- Jobs older than 1 hour removed
- Prevents memory leaks for forgotten jobs
