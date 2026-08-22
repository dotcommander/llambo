package api

import (
	"os"
	"strings"
	"testing"
)

const errorsPath = "../errors.md"

func TestErrorsDocumentationExists(t *testing.T) {
	_, err := os.Stat(errorsPath)
	if err != nil {
		t.Fatalf("docs/api/errors.md does not exist: %v", err)
	}
}

func TestErrorsHasHttpStatusCodes(t *testing.T) {
	content, err := os.ReadFile(errorsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/errors.md: %v", err)
	}

	text := string(content)

	// Check for required HTTP status codes
	requiredCodes := []string{"400", "401", "403", "404", "500", "502", "503"}
	for _, code := range requiredCodes {
		if !strings.Contains(text, "`"+code+"`") && !strings.Contains(text, "| `"+code+"`") {
			t.Errorf("errors.md does not document HTTP status code %s", code)
		}
	}

	// Check for HTTP status code section
	if !strings.Contains(text, "## HTTP Status Codes") {
		t.Error("errors.md does not have '## HTTP Status Codes' section")
	}
}

func TestErrorsHasJsonErrorFormat(t *testing.T) {
	content, err := os.ReadFile(errorsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/errors.md: %v", err)
	}

	text := string(content)

	// Check for JSON error structure
	if !strings.Contains(text, "```json") {
		t.Error("errors.md does not contain JSON code block")
	}

	// Check for error response structure
	if !strings.Contains(text, `"error": {`) {
		t.Error("errors.md does not show error response JSON structure")
	}

	if !strings.Contains(text, `"message"`) || !strings.Contains(text, `"type"`) {
		t.Error("errors.md does not show required error fields (message, type)")
	}
}

func TestErrorsHasRateLimitHandling(t *testing.T) {
	content, err := os.ReadFile(errorsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/errors.md: %v", err)
	}

	text := string(content)

	// Check for 429 handling
	if !strings.Contains(text, "429") && !strings.Contains(text, "Too Many Requests") {
		t.Error("errors.md does not document 429/Too Many Requests handling")
	}

	// Check for retry-after
	if !strings.Contains(text, "Retry-After") && !strings.Contains(text, "retry-after") {
		t.Error("errors.md does not document Retry-After header handling")
	}

	// Check for circuit breaker
	if !strings.Contains(text, "circuit breaker") && !strings.Contains(text, "Circuit Breaker") {
		t.Error("errors.md does not document circuit breaker behavior")
	}

	// Check for key rotation
	if !strings.Contains(text, "key rotation") && !strings.Contains(text, "Key rotation") {
		t.Error("errors.md does not document key rotation on 429")
	}
}

func TestErrorsHasErrorTypesTable(t *testing.T) {
	content, err := os.ReadFile(errorsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/errors.md: %v", err)
	}

	text := string(content)

	// Check for error types table
	if !strings.Contains(text, "| Type | Description | Example |") {
		t.Error("errors.md does not contain error types table header")
	}

	// Check for required error types
	requiredTypes := []string{"invalid_request", "upstream_error", "not_found", "rate_limit", "quota", "auth", "transient"}
	for _, typ := range requiredTypes {
		if !strings.Contains(text, "`"+typ+"`") {
			t.Errorf("errors.md does not document error type %s", typ)
		}
	}
}

func TestErrorsHasCircuitBreakerBehavior(t *testing.T) {
	content, err := os.ReadFile(errorsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/errors.md: %v", err)
	}

	text := string(content)

	// Check for circuit breaker details
	if !strings.Contains(text, "Failure Detection") || !strings.Contains(text, "Auto-Recovery") {
		t.Error("errors.md does not have detailed circuit breaker behavior sections")
	}

	// Check for cooldown times
	if !strings.Contains(text, "5 minutes") && !strings.Contains(text, "60 seconds") {
		t.Error("errors.md does not document circuit breaker cooldown times")
	}
}
