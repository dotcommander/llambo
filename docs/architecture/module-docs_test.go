// Verification tests for module-docs.md documentation
// This is a Go test file to verify the documentation meets acceptance criteria

package architecture_test

import (
	"os"
	"strings"
	"testing"
)

func TestModuleDocsConventionExists(t *testing.T) {
	// Acceptance Criterion 1: Given docs/architecture/module-docs.md exists
	_, err := os.Stat("module-docs.md")
	if err != nil {
		t.Errorf("docs/architecture/module-docs.md does not exist: %v", err)
	}
}

func TestModuleDocsConventionExplained(t *testing.T) {
	// Acceptance Criterion 1: when read, then the module documentation convention is explained
	content, err := os.ReadFile("module-docs.md")
	if err != nil {
		t.Fatalf("Failed to read module-docs.md: %v", err)
	}

	doc := string(content)

	// Test that Rust-style module documentation convention is explained
	conventionKeywords := []string{
		"Rust-style",
		"//!",
		"module documentation",
		"Module:",
		"Purpose:",
		"Usage:",
		"Examples:",
		"Dependencies:",
		"Key Types/Functions:",
		"Architecture Context:",
	}

	for _, keyword := range conventionKeywords {
		if !strings.Contains(doc, keyword) {
			t.Errorf("module-docs.md missing Rust-style convention keyword: %s", keyword)
		}
	}
}

func TestModuleDocsHasExamples(t *testing.T) {
	// Acceptance Criterion 2: Given the documentation, when checking examples, then sample Go file header comments showing purpose, usage, and examples are shown
	content, err := os.ReadFile("module-docs.md")
	if err != nil {
		t.Fatalf("Failed to read module-docs.md: %v", err)
	}

	doc := string(content)

	// Test that sample Go file header comments are shown
	examplePatterns := []string{
		"```go",
		"//! Module:",
		"cmd.serve",
		"gateway.server",
		"providers.openai_provider",
		"$ llambo serve",
		"server := gateway.New(configs)",
		"provider, err := providers.NewOpenAI(configs)",
	}

	for _, pattern := range examplePatterns {
		if !strings.Contains(doc, pattern) {
			t.Errorf("module-docs.md missing example pattern: %s", pattern)
		}
	}

	// Test that there are at least 3 complete examples
	if strings.Count(doc, "//! Module:") < 3 {
		t.Error("module-docs.md should have at least 3 complete module examples")
	}
}

func TestModuleDocsHasUsageGuidelines(t *testing.T) {
	// Acceptance Criterion 3: Given the convention guide, when a contributor reads it, then they understand how to document new modules
	content, err := os.ReadFile("module-docs.md")
	if err != nil {
		t.Fatalf("Failed to read module-docs.md: %v", err)
	}

	doc := string(content)

	// Test that usage guidelines for contributors are included
	guidelineKeywords := []string{
		"Implementation Guidelines",
		"File Location",
		"Content Guidelines",
		"Language",
		"Benefits",
		"Enforcement",
		"Validation",
		"self-documenting",
		"consistency",
		"onboarding",
	}

	for _, keyword := range guidelineKeywords {
		if !strings.Contains(doc, keyword) {
			t.Errorf("module-docs.md missing contributor guideline keyword: %s", keyword)
		}
	}

	// Test that the guide explains how to document new modules
	if !strings.Contains(doc, "All new .go files must include") ||
		!strings.Contains(doc, "Existing files should be updated when modified") {
		t.Error("module-docs.md missing instructions for documenting new modules")
	}
}

func TestModuleDocsStructureComplete(t *testing.T) {
	// Additional test: Verify the document has all required sections
	content, err := os.ReadFile("module-docs.md")
	if err != nil {
		t.Fatalf("Failed to read module-docs.md: %v", err)
	}

	doc := string(content)

	// Check for major sections
	sections := []string{
		"# Module Documentation Convention",
		"## Purpose",
		"## Rust-style Module Comments",
		"## Example Headers",
		"## Implementation Guidelines",
		"## Benefits",
		"## Enforcement",
		"## Validation",
		"## Related Documentation",
	}

	for _, section := range sections {
		if !strings.Contains(doc, section) {
			t.Errorf("module-docs.md missing section: %s", section)
		}
	}
}

func TestModuleDocsExamplesHaveRequiredSections(t *testing.T) {
	// Test that examples show all required sections
	content, err := os.ReadFile("module-docs.md")
	if err != nil {
		t.Fatalf("Failed to read module-docs.md: %v", err)
	}

	doc := string(content)

	// Find the example headers section
	exampleStart := strings.Index(doc, "### Command Module Example")
	if exampleStart == -1 {
		t.Fatal("module-docs.md missing example headers section")
	}

	// Extract examples section
	examplesSection := doc[exampleStart:]

	// Verify each example has required sections
	requiredSections := []string{
		"Module:",
		"Purpose:",
		"Usage:",
		"Examples:",
		"Dependencies:",
		"Key Types/Functions:",
		"Architecture Context:",
	}

	// Count occurrences in examples section
	for _, section := range requiredSections {
		count := strings.Count(examplesSection, section)
		if count < 3 {
			t.Errorf("Examples section missing required section '%s' in all examples (found %d times)", section, count)
		}
	}
}
