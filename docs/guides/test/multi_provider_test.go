package guides

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMultiProviderDocumentation verifies the multi-provider setup guide meets acceptance criteria
func TestMultiProviderDocumentation(t *testing.T) {
	// Get the path to the multi-provider guide
	guidePath := filepath.Join("..", "multi-provider.md")
	data, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatalf("Failed to read multi-provider guide: %v", err)
	}

	content := string(data)

	// Acceptance Criterion 1: Configuring 2+ providers is shown with complete example
	t.Run("CompleteExampleWithMultipleProviders", func(t *testing.T) {
		// Check for complete JSON example with 2+ providers
		if !strings.Contains(content, `"openai":`) {
			t.Error("Guide missing OpenAI provider example")
		}

		if !strings.Contains(content, `"openrouter":`) {
			t.Error("Guide missing OpenRouter provider example")
		}

		if !strings.Contains(content, `"gemini":`) {
			t.Error("Guide missing Gemini provider example")
		}

		// Verify the example shows 3 providers
		openaiCount := strings.Count(content, `"openai":`)
		openrouterCount := strings.Count(content, `"openrouter":`)
		geminiCount := strings.Count(content, `"gemini":`)

		if openaiCount < 2 {
			t.Error("Guide should show multiple OpenAI provider examples")
		}

		if openrouterCount < 2 {
			t.Error("Guide should show multiple OpenRouter provider examples")
		}

		if geminiCount < 2 {
			t.Error("Guide should show multiple Gemini provider examples")
		}

		// Verify complete JSON structure with all required fields
		requiredFields := []string{
			`"base_url":`,
			`"model":`,
			`"enabled":`,
			`"workers":`,
			`"priority":`,
		}

		for _, field := range requiredFields {
			if !strings.Contains(content, field) {
				t.Errorf("Guide missing required field: %s", field)
			}
		}

		t.Log("✓ Guide contains complete example with 2+ providers")
	})

	// Acceptance Criterion 2: Priority field and load balancing behavior is explained
	t.Run("PriorityAndLoadBalancingExplained", func(t *testing.T) {
		// Check for priority explanation
		if !strings.Contains(content, "priority") && !strings.Contains(content, "Priority") {
			t.Error("Guide missing explanation of priority field")
		}

		// Check for load balancing explanation
		if !strings.Contains(strings.ToLower(content), "load balancing") &&
			!strings.Contains(strings.ToLower(content), "load balanc") {
			t.Error("Guide missing explanation of load balancing")
		}

		// Check for specific priority behavior explanation
		priorityKeywords := []string{
			"lower = higher priority",
			"lower values",
			"higher priority",
			"priority order",
		}

		found := false
		for _, keyword := range priorityKeywords {
			if strings.Contains(strings.ToLower(content), strings.ToLower(keyword)) {
				found = true
				break
			}
		}

		if !found {
			t.Error("Guide missing explanation of how priority values affect provider selection")
		}

		// Check for round-robin explanation for same priority
		if !strings.Contains(strings.ToLower(content), "round-robin") &&
			!strings.Contains(strings.ToLower(content), "round robin") {
			t.Error("Guide missing explanation of round-robin behavior for same priority")
		}

		t.Log("✓ Guide explains priority field and load balancing behavior")
	})

	// Acceptance Criterion 3: Automatic failover chain is documented
	t.Run("AutomaticFailoverChainDocumented", func(t *testing.T) {
		// Check for failover explanation
		if !strings.Contains(strings.ToLower(content), "failover") {
			t.Error("Guide missing failover explanation")
		}

		// Check for circuit breaker explanation
		if !strings.Contains(strings.ToLower(content), "circuit breaker") &&
			!strings.Contains(strings.ToLower(content), "circuit-breaker") {
			t.Error("Guide missing circuit breaker explanation")
		}

		// Check for automatic recovery explanation
		if !strings.Contains(strings.ToLower(content), "automatic recovery") &&
			!strings.Contains(strings.ToLower(content), "auto-recovery") &&
			!strings.Contains(strings.ToLower(content), "re-enable") {
			t.Error("Guide missing automatic recovery explanation")
		}

		// Check for failover chain examples
		if !strings.Contains(content, "primary") || !strings.Contains(content, "secondary") {
			t.Error("Guide missing primary/secondary failover chain examples")
		}

		// Check for health-based routing
		if !strings.Contains(strings.ToLower(content), "health") &&
			!strings.Contains(strings.ToLower(content), "healthy") {
			t.Error("Guide missing health-based routing explanation")
		}

		// Check for API key rotation explanation
		if !strings.Contains(strings.ToLower(content), "api key rotation") &&
			!strings.Contains(strings.ToLower(content), "key rotation") {
			t.Error("Guide missing API key rotation explanation")
		}

		t.Log("✓ Guide documents automatic failover chain")
	})

	// Additional validation: Guide structure and completeness
	t.Run("GuideStructure", func(t *testing.T) {
		// Check for sections
		sections := []string{
			"## Overview",
			"## Basic Multi-Provider Configuration",
			"## Priority-Based Load Balancing",
			"## Automatic Failover Chain",
			"## Complete Multi-Provider Examples",
		}

		for _, section := range sections {
			if !strings.Contains(content, section) {
				t.Errorf("Guide missing section: %s", section)
			}
		}

		// Check for example configurations
		if strings.Count(content, "```json") < 5 {
			t.Error("Guide should have multiple JSON configuration examples")
		}

		// Check for tables explaining concepts
		if strings.Count(content, "|") < 10 {
			t.Error("Guide should use tables to explain concepts")
		}

		t.Log("✓ Guide has proper structure and sections")
	})
}
