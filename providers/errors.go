package providers

import (
	"regexp"
	"strconv"
	"strings"
)

// =============================================================================
// Helper Functions
// =============================================================================
// These functions are shared across error pattern, type, and classification files.
// =============================================================================

// httpStatusPattern matches HTTP status codes in error messages.
// Matches explicit HTTP or status contexts, never unrelated model numbers.
var httpStatusPattern = regexp.MustCompile(`(?i)(?:http(?:/\d(?:\.\d)?)?\s+|status(?:_code|\s+code)?[\s:=]+)([1-5][0-9]{2})(?:\b)`)

// extractHTTPStatus attempts to extract an HTTP status code from an error message.
// Returns 0 if no valid status code is found.
func extractHTTPStatus(msg string) int {
	// A status-only error is unambiguous; numbers embedded in ordinary text are not.
	trimmed := strings.TrimSpace(msg)
	if len(trimmed) == 3 {
		if code, err := strconv.Atoi(trimmed); err == nil && code >= 100 && code < 600 {
			return code
		}
	}
	matches := httpStatusPattern.FindAllStringSubmatch(msg, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			code, err := strconv.Atoi(match[1])
			if err == nil && code >= 100 && code < 600 {
				return code
			}
		}
	}
	return 0
}

// containsAny checks if msg contains any of the patterns (case-insensitive).
func containsAny(msg string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// containsInt checks if slice contains the given value.
func containsInt(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}
