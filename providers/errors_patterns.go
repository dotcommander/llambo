package providers

// =============================================================================
// Error Pattern Constants
// =============================================================================
// These constants define the error patterns used for classification.
// To add new patterns, add them to the appropriate category slice.
// This design follows Open/Closed principle - extend by adding to slices,
// not by modifying classification logic.
// =============================================================================

// HTTP status codes that indicate rate limiting or quota issues.
// These are checked first before string pattern matching.
var RateLimitStatusCodes = []int{
	429, // Too Many Requests - standard rate limit response
}

// HTTP status codes that indicate authentication/authorization errors.
var AuthErrorStatusCodes = []int{
	401, // Unauthorized - invalid or missing API key
	403, // Forbidden - valid key but insufficient permissions
}

// HTTP status codes that indicate transient server errors worth retrying.
var TransientStatusCodes = []int{
	502, // Bad Gateway - upstream server error
	503, // Service Unavailable - server overloaded
	504, // Gateway Timeout - upstream timeout
}

// RateLimitPatterns contains string patterns indicating rate limiting.
// These are checked when HTTP status code is not available in the error.
//
// Patterns included:
// - "rate limit", "rate_limit": Standard rate limit messages
// - "too many requests": Human-readable rate limit
// - "throttl": Throttling messages (matches "throttle", "throttled", "throttling")
var RateLimitPatterns = []string{
	"rate limit",
	"rate_limit",
	"too many requests",
	"throttl", // matches throttle, throttled, throttling
}

// QuotaPatterns contains string patterns indicating quota exhaustion.
// These typically require waiting longer than rate limits.
//
// Patterns included:
// - "quota": General quota exceeded messages
// - "exceeded your current quota": OpenAI specific message
// - "billing": Billing/payment issues that block requests
// - "insufficient_quota": OpenAI API error code
var QuotaPatterns = []string{
	"quota",
	"exceeded your current quota",
	"billing",
	"insufficient_quota",
}

// AuthPatterns contains string patterns indicating authentication errors.
// These are permanent errors requiring configuration fixes.
//
// Patterns included:
// - "invalid api key", "incorrect api key": Wrong API key
// - "authentication", "unauthorized": Auth failure messages
// - "api key not found": Missing key
// - "invalid_api_key": API error codes
var AuthPatterns = []string{
	"invalid api key",
	"incorrect api key",
	"authentication",
	"unauthorized",
	"api key not found",
	"invalid_api_key",
}

// ConfigPatterns contains string patterns indicating configuration errors.
// These are permanent errors requiring code or config changes.
//
// Patterns included:
// - "unsupported value/parameter": Invalid request parameters
// - "invalid model", "model not found": Wrong model identifier
// - "does not exist": Resource not found
// - "invalid_model": API error codes
var ConfigPatterns = []string{
	"unsupported value",
	"unsupported parameter",
	"invalid model",
	"model not found",
	"does not exist",
	"invalid_model",
}

// ModelUnsupportedPatterns contains string patterns indicating the selected
// model identifier is not usable on this provider.
var ModelUnsupportedPatterns = []string{
	"invalid model",
	"model not found",
	"model does not exist",
	"model is not supported",
	"not a valid model",
	"invalid_model",
	"model_not_found",
}

// TransientPatterns contains string patterns indicating recoverable errors.
// These are worth retrying after a short delay.
//
// Patterns included:
// - Connection errors: reset, refused, closed
// - Timeout errors: i/o timeout, tls handshake timeout
// - Temporary failures: eof, temporary failure
var TransientPatterns = []string{
	"bad gateway",
	"service unavailable",
	"gateway timeout",
	"server closed connection",
	"connection reset",
	"connection refused",
	"eof",
	"temporary failure",
	"i/o timeout",
	"tls handshake timeout",
	"context deadline exceeded",
	"no such host",
	"network is unreachable",
}
