package getting_started

import (
	"os"
	"strings"
	"testing"
)

const quickstartPath = "quickstart.md"

func TestQuickstartGuideExists(t *testing.T) {
	_, err := os.Stat(quickstartPath)
	if err != nil {
		t.Fatalf("quickstart.md does not exist: %v", err)
	}
}

func TestQuickstartFollowsInstallConfigRunTestFlow(t *testing.T) {
	content, err := os.ReadFile(quickstartPath)
	if err != nil {
		t.Fatalf("Failed to read quickstart.md: %v", err)
	}

	text := string(content)

	// Check for the 4-step flow mentioned in the guide
	requiredSections := []string{
		"Step 1: Install Llambo",
		"Step 2: Create Basic Configuration",
		"Step 3: Start the Gateway",
		"Step 4: Test with Your First API Call",
	}

	for _, section := range requiredSections {
		if !strings.Contains(text, section) {
			t.Errorf("quickstart.md missing required section: %s", section)
		}
	}

	// Verify each step has appropriate content
	lines := strings.Split(text, "\n")

	// Check Step 1 has installation commands
	foundStep1 := false
	foundInstallCommand := false
	for i, line := range lines {
		if strings.Contains(line, "Step 1: Install Llambo") {
			foundStep1 = true
			// Look for installation commands in subsequent lines
			for j := i + 1; j < min(i+20, len(lines)); j++ {
				if strings.Contains(lines[j], "go build -o llambo .") &&
					strings.Contains(lines[j], "ln -sf $(pwd)/llambo ~/go/bin/llambo") {
					foundInstallCommand = true
					break
				}
			}
			break
		}
	}

	if !foundStep1 {
		t.Error("Step 1 section not found")
	}
	if !foundInstallCommand {
		t.Error("Step 1 missing installation commands")
	}
}

func TestQuickstartIncludesHealthCheckCurlCommand(t *testing.T) {
	content, err := os.ReadFile(quickstartPath)
	if err != nil {
		t.Fatalf("Failed to read quickstart.md: %v", err)
	}

	text := string(content)

	// Check for health endpoint curl command
	if !strings.Contains(text, "curl http://localhost:8080/health") {
		t.Error("quickstart.md does not include curl command to /health endpoint")
	}

	// Check that it's in the testing section
	lines := strings.Split(text, "\n")
	inTestingSection := false
	foundHealthCommand := false

	for _, line := range lines {
		if strings.Contains(line, "Step 4: Test with Your First API Call") ||
			strings.Contains(line, "Test with Your First API Call") {
			inTestingSection = true
		}

		if inTestingSection && strings.Contains(line, "curl http://localhost:8080/health") {
			foundHealthCommand = true
			break
		}
	}

	if !foundHealthCommand {
		t.Error("Health check curl command not found in testing section")
	}
}

func TestQuickstartProvidesWorkingApiCallExample(t *testing.T) {
	content, err := os.ReadFile(quickstartPath)
	if err != nil {
		t.Fatalf("Failed to read quickstart.md: %v", err)
	}

	text := string(content)

	// Check for at least one API call example that should work
	// Either health endpoint or chat completion
	hasWorkingExample := false

	// Health endpoint should always work
	if strings.Contains(text, "curl http://localhost:8080/health") {
		hasWorkingExample = true
	}

	// Or chat completion with local provider
	if strings.Contains(text, "curl -X POST http://localhost:8080/v1/chat/completions") {
		// Check if it mentions this works with local provider
		if strings.Contains(text, "local-model") ||
			strings.Contains(text, "lmstudio") ||
			strings.Contains(text, "requires_key: false") {
			hasWorkingExample = true
		}
	}

	if !hasWorkingExample {
		t.Error("quickstart.md does not provide a working API call example")
	}
}

func TestQuickstartCanBeCompletedUnder5Minutes(t *testing.T) {
	content, err := os.ReadFile(quickstartPath)
	if err != nil {
		t.Fatalf("Failed to read quickstart.md: %v", err)
	}

	text := string(content)

	// Check that the guide claims to be under 5 minutes
	if !strings.Contains(text, "5 minutes") && !strings.Contains(text, "under 5 minutes") {
		t.Error("quickstart.md does not state it can be completed in under 5 minutes")
	}

	// Check for minimal steps - should not have complex setup
	lines := strings.Split(text, "\n")
	stepCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "## Step ") {
			stepCount++
		}
	}

	// Should have 4 steps as promised
	if stepCount != 4 {
		t.Errorf("quickstart.md should have 4 steps, found %d", stepCount)
	}

	// Check guide doesn't require complex setup like installing LM Studio
	// as a mandatory step
	requiresComplexSetup := false
	if strings.Contains(text, "Install and run [LM Studio]") &&
		!strings.Contains(text, "skip to Step 4") &&
		!strings.Contains(text, "If you don't have") {
		requiresComplexSetup = true
	}

	if requiresComplexSetup {
		t.Error("quickstart.md requires complex setup (LM Studio) without providing alternative")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
