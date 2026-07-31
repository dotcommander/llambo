package providers

import "time"

// OpenAIError wraps an OpenAI SDK error while preserving error chain
type OpenAIError struct {
	message    string
	statusCode int           // HTTP status code, 0 if unknown
	retryAfter time.Duration // provider-suggested cooldown, 0 if unknown
	source     error         // underlying error
}

func (e *OpenAIError) Error() string {
	return e.message
}

// StatusCode returns the HTTP status code if available, 0 otherwise
func (e *OpenAIError) StatusCode() int {
	return e.statusCode
}

// RetryAfter returns the provider-suggested cooldown duration, if one was
// preserved from retry/reset metadata.
func (e *OpenAIError) RetryAfter() time.Duration {
	return e.retryAfter
}

// Unwrap returns the underlying error for errors.Is/errors.As
func (e *OpenAIError) Unwrap() error {
	return e.source
}

// NewOpenAIError creates a wrapped error with message, status code, and source
func NewOpenAIError(message string, statusCode int, source error) *OpenAIError {
	return &OpenAIError{
		message:    message,
		statusCode: statusCode,
		retryAfter: ExtractRetryAfter(message, time.Now()),
		source:     source,
	}
}

// NewOpenAIErrorFromMessage creates an error from just a message, extracting status code if present
func NewOpenAIErrorFromMessage(message string) *OpenAIError {
	return &OpenAIError{
		message:    message,
		statusCode: extractHTTPStatus(message),
		retryAfter: ExtractRetryAfter(message, time.Now()),
		source:     nil,
	}
}

// NewOpenAIErrorWithRetryAfter creates a wrapped provider error with explicit
// retry metadata. It is useful for adapters that can preserve HTTP reset
// headers directly instead of relying on message parsing.
func NewOpenAIErrorWithRetryAfter(message string, statusCode int, retryAfter time.Duration, source error) *OpenAIError {
	return &OpenAIError{
		message:    message,
		statusCode: statusCode,
		retryAfter: retryAfter,
		source:     source,
	}
}

// NoContentResponseError preserves metadata from a successful provider response
// that carried no assistant-visible text or tool call.
type NoContentResponseError struct {
	Model        string
	FinishReason string
	Usage        *LLMUsage
}

func (e *NoContentResponseError) Error() string {
	return e.Model + ": no content in response"
}
