package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dotcommander/llambo/providers"
)

var (
	statusCodeRegex = regexp.MustCompile(`\b([1-5][0-9]{2})\b`)
	idCounter       atomic.Uint64
)

func generateID(prefix string) string {
	b := make([]byte, IDRandomBytes)
	if _, err := rand.Read(b); err != nil {
		n := idCounter.Add(1)
		return fmt.Sprintf("%s-%d-%06d", prefix, time.Now().UnixMilli(), n%1000000)
	}
	return prefix + "-" + hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONWithProviderHeaders(w http.ResponseWriter, status int, v interface{}, provider, model string) {
	w.Header().Set("Content-Type", "application/json")
	if provider != "" {
		w.Header().Set("X-Llambo-Provider", provider)
	}
	if model != "" {
		w.Header().Set("X-Llambo-Model", model)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// sanitizeUpstreamError keeps raw provider response bodies and request context
// out of client-facing errors by returning only fixed category messages.
func sanitizeUpstreamError(err error) (errType, message string) {
	switch providers.ClassifyError(err) {
	case providers.RateLimitError:
		return "rate_limit", "Upstream provider rate limit exceeded"
	case providers.QuotaError:
		return "quota", "Upstream provider quota exhausted"
	case providers.AuthError:
		return "auth", "Upstream provider authentication failed"
	case providers.TransientError:
		return "transient", "Upstream provider temporarily unavailable"
	default:
		return "upstream_error", "Upstream provider request failed"
	}
}

func upstreamErrorDetail(err error) ErrorDetail {
	errType, message := sanitizeUpstreamError(err)
	detail := ErrorDetail{Message: message, Type: errType}
	var noContent *providers.NoContentResponseError
	if errors.As(err, &noContent) {
		detail.Type = "no_content"
		detail.Message = "Upstream provider returned no assistant-visible content"
		detail.FinishReason = normalizeOpenAIFinishReason(noContent.FinishReason)
		if noContent.Usage != nil {
			detail.PromptTokens = noContent.Usage.PromptTokens
			detail.CompletionTokens = noContent.Usage.CompletionTokens
			detail.TotalTokens = noContent.Usage.TotalTokens
		}
	}
	return detail
}

func writeError(w http.ResponseWriter, status int, errType, message string) {
	writeErrorDetail(w, status, ErrorDetail{Message: message, Type: errType})
}

func writeErrorDetail(w http.ResponseWriter, status int, detail ErrorDetail) {
	writeJSON(w, status, ErrorResponse{Error: detail})
}

func writeAnthropicError(w http.ResponseWriter, status int, requestID, message string) {
	writeJSON(w, status, AnthropicErrorResponse{
		Type: "error", Error: AnthropicErrorDetail{Type: anthropicErrorTypeFromStatus(status), Message: message}, RequestID: requestID,
	})
}

func anthropicStatusFromError(err error) int {
	if err == nil {
		return http.StatusBadGateway
	}
	var oe *providers.OpenAIError
	if errors.As(err, &oe) {
		if code := oe.StatusCode(); code >= 400 && code <= 599 {
			return code
		}
	}
	msg := strings.ToLower(err.Error())
	for _, match := range statusCodeRegex.FindAllStringSubmatch(msg, -1) {
		if len(match) >= 2 {
			if code, convErr := strconv.Atoi(match[1]); convErr == nil && code >= 400 && code <= 599 {
				return code
			}
		}
	}
	switch {
	case strings.Contains(msg, "rate limit"):
		return http.StatusTooManyRequests
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "authentication"):
		return http.StatusUnauthorized
	case strings.Contains(msg, "forbidden"), strings.Contains(msg, "permission"):
		return http.StatusForbidden
	case strings.Contains(msg, "not found"):
		return http.StatusNotFound
	case strings.Contains(msg, "overloaded"):
		return 529
	case strings.Contains(msg, "timeout"):
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}
