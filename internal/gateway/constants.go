package gateway

import (
	"time"

	"github.com/dotcommander/llambo/providers"
)

// HTTP server timeout constants
const (
	// ServerReadTimeout is the maximum duration for reading the entire request
	ServerReadTimeout = 30 * time.Second

	// ServerWriteTimeout is the maximum duration for writing the response.
	// Set higher to accommodate slow LLM responses.
	ServerWriteTimeout = 120 * time.Second

	// ServerIdleTimeout is the maximum duration to wait for the next request
	ServerIdleTimeout = 60 * time.Second

	// HandlerTimeout is the maximum duration for a single handler request.
	// Should be less than ServerWriteTimeout to allow error response.
	HandlerTimeout = 90 * time.Second

	// MaxRequestBodySize is the maximum allowed request body size (10MB)
	MaxRequestBodySize = 10 << 20
)

// Server lifecycle constants
const (
	// ShutdownTimeout is the maximum time to wait for graceful shutdown.
	// Must exceed HandlerTimeout so in-flight streaming requests can finalize
	// rather than being forcefully closed mid-stream by httpServer.Shutdown.
	ShutdownTimeout = HandlerTimeout + 5*time.Second
)

// Job management constants
const (
	// JobCleanupInterval is how often the cleanup goroutine runs
	JobCleanupInterval = 5 * time.Minute

	// JobMaxAge is how long completed jobs are retained before cleanup
	JobMaxAge = 1 * time.Hour

	// JobMaxActiveDefault is the default cap for concurrently active jobs
	// (pending + processing). New jobs are rejected when this limit is reached.
	JobMaxActiveDefault = providers.DefaultMaxActiveJobs

	// JobMaxRequestsPerJob is the maximum number of requests allowed
	// in a single /v1/jobs submission.
	JobMaxRequestsPerJob = providers.DefaultMaxRequestsPerJob
)

// Token estimation constants
const (
	// TokenEstimationMultiplier is a rough estimate of tokens per input item.
	// Used for embedding usage statistics when actual token counts are unavailable.
	TokenEstimationMultiplier = 4
)

// ID generation constants
const (
	// IDRandomBytes is the number of random bytes used for generating unique IDs
	IDRandomBytes = 12
)

// OpenAI SSE object type constants
const (
	// ObjectChatCompletionChunk is the SSE object type for streaming chat completion chunks.
	ObjectChatCompletionChunk = "chat.completion.chunk"
)

// JobStatus represents the state of a job
type JobStatus string

// Job status values
const (
	JobStatusPending        JobStatus = "pending"
	JobStatusProcessing     JobStatus = "processing"
	JobStatusCompleted      JobStatus = "completed"
	JobStatusPartialFailure JobStatus = "partial_failure"
	JobStatusFailed         JobStatus = "failed"
	JobStatusCancelled      JobStatus = "cancelled"
)

// IsTerminal returns true if the status is a final state
func (s JobStatus) IsTerminal() bool {
	return s == JobStatusCompleted || s == JobStatusPartialFailure || s == JobStatusFailed || s == JobStatusCancelled
}

// ResultStatus represents the state of an individual job result
type ResultStatus string

// Result status values
const (
	ResultStatusCompleted ResultStatus = "completed"
	ResultStatusFailed    ResultStatus = "failed"
)
