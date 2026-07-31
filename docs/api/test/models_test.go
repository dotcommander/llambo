package api

import (
	"os"
	"strings"
	"testing"
)

const modelsPath = "../models.md"

func TestModelsDocumentationExists(t *testing.T) {
	_, err := os.Stat(modelsPath)
	if err != nil {
		t.Fatalf("docs/api/models.md does not exist: %v", err)
	}
}

func TestModelsHasEndpointMethodAndPath(t *testing.T) {
	content, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/models.md: %v", err)
	}

	text := string(content)

	// Check for endpoint section
	if !strings.Contains(text, "## Endpoint") {
		t.Error("Documentation missing '## Endpoint' section")
	}

	// Check for GET method in endpoint
	if !strings.Contains(text, "```http\nGET /v1/models") {
		t.Error("Endpoint documentation missing GET method and /v1/models path")
	}

	// Check that endpoint is clearly documented
	if !strings.Contains(text, "GET /v1/models") {
		t.Error("Endpoint method (GET) and path (/v1/models) not clearly documented")
	}
}

func TestModelsHasOpenAICompatibleResponseFormat(t *testing.T) {
	content, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/models.md: %v", err)
	}

	text := string(content)

	// Check for response schema section
	if !strings.Contains(text, "## Response Schema") {
		t.Error("Documentation missing '## Response Schema' section")
	}

	// Check for ModelsResponse
	if !strings.Contains(text, "### ModelsResponse") {
		t.Error("Documentation missing '### ModelsResponse' section")
	}

	// Check for ModelInfo
	if !strings.Contains(text, "### ModelInfo Object") {
		t.Error("Documentation missing '### ModelInfo Object' section")
	}

	// Check that all required fields from types.go are documented
	modelsResponseFields := []string{
		"object",
		"data",
	}

	for _, field := range modelsResponseFields {
		// Look for field in table format: | `field` |
		tablePattern := "| `" + field + "` |"
		if !strings.Contains(text, tablePattern) {
			t.Errorf("ModelsResponse field '%s' not documented in response schema table", field)
		}
	}

	// Check ModelInfo fields
	modelInfoFields := []string{"id", "object", "owned_by"}
	for _, field := range modelInfoFields {
		tablePattern := "| `" + field + "` |"
		if !strings.Contains(text, tablePattern) {
			t.Errorf("ModelInfo field '%s' not documented in model info table", field)
		}
	}

	// Check that response format matches OpenAI compatibility
	if !strings.Contains(text, "OpenAI's models list format") {
		t.Error("Documentation doesn't mention OpenAI compatibility")
	}

	// Check for object: "list" in examples
	if !strings.Contains(text, `"object": "list"`) {
		t.Error("Documentation missing 'object: \"list\"' in response examples")
	}

	// Check for object: "model" in examples
	if !strings.Contains(text, `"object": "model"`) {
		t.Error("Documentation missing 'object: \"model\"' in response examples")
	}
}

func TestModelsHasCurlExamples(t *testing.T) {
	content, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/models.md: %v", err)
	}

	text := string(content)

	// Check for examples section
	if !strings.Contains(text, "## Examples") {
		t.Error("Documentation missing '## Examples' section")
	}

	// Check for at least one curl example
	if !strings.Contains(text, "```bash\ncurl -X GET") {
		t.Error("Documentation missing curl example with GET method")
	}

	// Check that example includes expected response
	if !strings.Contains(text, "```json\n{") {
		t.Error("Documentation missing JSON response example")
	}

	// Check that response example has basic structure
	requiredResponseFields := []string{
		`"object": "list"`,
		`"data": [`,
		`"id":`,
		`"owned_by":`,
	}

	for _, field := range requiredResponseFields {
		if !strings.Contains(text, field) {
			t.Errorf("Response example missing field: %s", field)
		}
	}

	// Check for multiple provider example
	if !strings.Contains(text, `"owned_by": "openai"`) && !strings.Contains(text, `"owned_by": "openrouter"`) {
		t.Error("Documentation missing example with multiple providers")
	}
}

func TestModelsHasCompleteDocumentationStructure(t *testing.T) {
	content, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/models.md: %v", err)
	}

	text := string(content)

	// Check for main sections (adapted for models endpoint)
	requiredSections := []string{
		"# Models",
		"## Endpoint",
		"## Overview",
		"## Response Schema",
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
