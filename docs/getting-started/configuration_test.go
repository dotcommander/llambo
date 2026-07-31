package getting_started

import (
	"os"
	"strings"
	"testing"
)

const configurationPath = "configuration.md"

func TestConfigurationGuideExists(t *testing.T) {
	_, err := os.Stat(configurationPath)
	if err != nil {
		t.Fatalf("configuration.md does not exist: %v", err)
	}
}

func TestConfigFileLocationExplained(t *testing.T) {
	content, err := os.ReadFile(configurationPath)
	if err != nil {
		t.Fatalf("Failed to read configuration.md: %v", err)
	}

	text := string(content)

	// Check for config file location explanation
	if !strings.Contains(text, "~/.config/llambo/config.json") {
		t.Error("configuration.md does not explain config file location (~/.config/llambo/config.json)")
	}
}

func TestCompleteExampleConfigShown(t *testing.T) {
	content, err := os.ReadFile(configurationPath)
	if err != nil {
		t.Fatalf("Failed to read configuration.md: %v", err)
	}

	text := string(content)

	// Check for complete example with all required fields
	requiredFields := []string{
		"\"base_url\"",
		"\"model\"",
		"\"enabled\"",
		"\"api_keys\"",
		"\"workers\"",
		"\"priority\"",
		"\"env_var\"",
		"\"requires_key\"",
		"\"extra_headers\"",
		"\"api_path\"",
	}

	// Find the main example section (assuming it's in a code block)
	exampleStart := strings.Index(text, "```json")
	if exampleStart == -1 {
		t.Error("configuration.md does not contain a JSON example")
		return
	}

	// Search for closing ``` after the opening ```json
	exampleEnd := strings.Index(text[exampleStart+7:], "```")
	if exampleEnd == -1 {
		t.Error("configuration.md JSON example is not properly closed")
		return
	}

	example := text[exampleStart+7 : exampleStart+7+exampleEnd] // +7 to skip "```json"

	for _, field := range requiredFields {
		if !strings.Contains(example, field) {
			t.Errorf("configuration.md example missing field: %s", field)
		}
	}
}

func TestAllConfigFieldsDocumented(t *testing.T) {
	content, err := os.ReadFile(configurationPath)
	if err != nil {
		t.Fatalf("Failed to read configuration.md: %v", err)
	}

	text := string(content)

	// All fields from CLAUDE.md that should be documented
	claudeFields := []struct {
		name     string
		required bool
	}{
		{"base_url", true},
		{"model", true},
		{"enabled", true},
		{"api_keys", false},
		{"env_var", false},
		{"workers", false},
		{"priority", false},
		{"max_tokens", false},
		{"extra_headers", false},
		{"requires_key", false},
		{"api_path", false},
	}

	// Check for configuration fields table or section
	if !strings.Contains(text, "Configuration Fields") && !strings.Contains(text, "Fields Reference") {
		t.Error("configuration.md does not have a configuration fields reference section")
		return
	}

	// Check each field is documented
	for _, field := range claudeFields {
		if !strings.Contains(text, field.name) {
			t.Errorf("configuration.md does not document field: %s", field.name)
		}
	}

	// Check that types and defaults are mentioned (not exhaustive, but should have indication)
	if !strings.Contains(text, "Type") || !strings.Contains(text, "Default") {
		t.Error("configuration.md does not include Type and Default columns for fields")
	}
}
