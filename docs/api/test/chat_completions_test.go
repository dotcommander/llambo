package api

import (
	"os"
	"strings"
	"testing"
)

const chatCompletionsPath = "../chat-completions.md"

func TestChatCompletionsDocumentationExists(t *testing.T) {
	_, err := os.Stat(chatCompletionsPath)
	if err != nil {
		t.Fatalf("docs/api/chat-completions.md does not exist: %v", err)
	}
}

func TestChatCompletionsHasRequestBodySchema(t *testing.T) {
	content, err := os.ReadFile(chatCompletionsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/chat-completions.md: %v", err)
	}

	text := string(content)

	// Check for request body schema section
	if !strings.Contains(text, "## Request Body Schema") {
		t.Error("Documentation missing '## Request Body Schema' section")
	}

	// Check for ChatCompletionRequest table
	if !strings.Contains(text, "### ChatCompletionRequest") {
		t.Error("Documentation missing '### ChatCompletionRequest' section")
	}

	// Check that all required fields from types.go are documented
	requiredFields := []string{
		"model",
		"messages",
		"temperature",
		"max_tokens",
		"stream",
	}

	for _, field := range requiredFields {
		// Look for field in table format: | `field` |
		tablePattern := "| `" + field + "` |"
		if !strings.Contains(text, tablePattern) {
			t.Errorf("Field '%s' not documented in request body schema table", field)
		}
	}

	// Check that Message object is documented
	if !strings.Contains(text, "### Message Object") {
		t.Error("Documentation missing '### Message Object' section")
	}

	// Check Message object fields
	messageFields := []string{"role", "content"}
	for _, field := range messageFields {
		tablePattern := "| `" + field + "` |"
		if !strings.Contains(text, tablePattern) {
			t.Errorf("Message field '%s' not documented in message object table", field)
		}
	}
}

func TestChatCompletionsHasCurlExamples(t *testing.T) {
	content, err := os.ReadFile(chatCompletionsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/chat-completions.md: %v", err)
	}

	text := string(content)

	// Check for examples section
	if !strings.Contains(text, "## Examples") {
		t.Error("Documentation missing '## Examples' section")
	}

	// Check for at least one curl example
	if !strings.Contains(text, "```bash\ncurl -X POST") {
		t.Error("Documentation missing curl example")
	}

	// Check that example includes expected response
	if !strings.Contains(text, "```json\n{") {
		t.Error("Documentation missing JSON response example")
	}

	// Check that response example has basic structure
	requiredResponseFields := []string{
		`"id":`,
		`"object": "chat.completion"`,
		`"choices":`,
		`"message":`,
	}

	for _, field := range requiredResponseFields {
		if !strings.Contains(text, field) {
			t.Errorf("Response example missing field: %s", field)
		}
	}
}

func TestChatCompletionsDocumentsResponseHeaders(t *testing.T) {
	content, err := os.ReadFile(chatCompletionsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/chat-completions.md: %v", err)
	}

	text := string(content)

	// Check for response headers section
	if !strings.Contains(text, "#### Response Headers") {
		t.Error("Documentation missing '#### Response Headers' section")
	}

	// Check for X-Llambo-Provider header documentation
	if !strings.Contains(text, "`X-Llambo-Provider`") {
		t.Error("Documentation missing X-Llambo-Provider header")
	}

	// Check for X-Llambo-Model header documentation
	if !strings.Contains(text, "`X-Llambo-Model`") {
		t.Error("Documentation missing X-Llambo-Model header")
	}

	// Check that headers are documented in a table
	if !strings.Contains(text, "| `X-Llambo-Provider` |") || !strings.Contains(text, "| `X-Llambo-Model` |") {
		t.Error("Headers not documented in table format")
	}

	// Check for separate Headers section
	if !strings.Contains(text, "## Headers") {
		t.Error("Documentation missing '## Headers' section")
	}

	// Check that response headers are documented in the Headers section
	headersSectionIndex := strings.Index(text, "## Headers")
	if headersSectionIndex >= 0 {
		headersSubtext := text[headersSectionIndex:]
		if !strings.Contains(headersSubtext, "### Response Headers") {
			t.Error("Headers section missing '### Response Headers' subsection")
		}
		if !strings.Contains(headersSubtext, "| `X-Llambo-Provider` |") || !strings.Contains(headersSubtext, "| `X-Llambo-Model` |") {
			t.Error("Response headers not documented in Headers section table")
		}
	}
}

func TestChatCompletionsHasCompleteDocumentationStructure(t *testing.T) {
	content, err := os.ReadFile(chatCompletionsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/chat-completions.md: %v", err)
	}

	text := string(content)

	// Check for main sections
	requiredSections := []string{
		"# Chat Completions",
		"## Endpoint",
		"## Overview",
		"## Request Body Schema",
		"## Examples",
		"## Response",
		"## Headers",
		"## Constraints and Limits",
		"## Behavior Notes",
		"## Common Use Cases",
		"## Integration Notes",
	}

	for _, section := range requiredSections {
		if !strings.Contains(text, section) {
			t.Errorf("Documentation missing section: %s", section)
		}
	}
}
