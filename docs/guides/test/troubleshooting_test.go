package guides

import (
	"os"
	"strings"
	"testing"
)

const troubleshootingPath = "../troubleshooting.md"

func TestTroubleshootingGuideExists(t *testing.T) {
	_, err := os.Stat(troubleshootingPath)
	if err != nil {
		t.Fatalf("docs/guides/troubleshooting.md does not exist: %v", err)
	}
}

func TestTroubleshootingGuideHasCommonIssuesFromCLAUDE(t *testing.T) {
	content, err := os.ReadFile(troubleshootingPath)
	if err != nil {
		t.Fatalf("Failed to read docs/guides/troubleshooting.md: %v", err)
	}

	text := string(content)

	// Check for the three common issues from CLAUDE.md
	issues := []string{
		"HTTP 404 from OpenAI-compatible Provider",
		"Provider Uses Wrong BaseURL",
		"Changes Don't Take Effect After Build",
	}

	for _, issue := range issues {
		if !strings.Contains(text, issue) {
			t.Errorf("Troubleshooting guide missing common issue from CLAUDE.md: %s", issue)
		}
	}
}

func TestTroubleshootingGuideHasSymptomCauseSolutionFormat(t *testing.T) {
	content, err := os.ReadFile(troubleshootingPath)
	if err != nil {
		t.Fatalf("Failed to read docs/guides/troubleshooting.md: %v", err)
	}

	text := string(content)

	// Check for symptom, cause, solution format for each issue
	// Look for table headers or structured format
	if !strings.Contains(text, "Symptom") || !strings.Contains(text, "Cause") || !strings.Contains(text, "Solution") {
		t.Error("Troubleshooting guide missing symptom/cause/solution format")
	}

	// Check that the format is applied to each issue section
	// We should see the pattern repeated for each issue
	symptomCount := strings.Count(text, "Symptom")
	if symptomCount < 3 {
		t.Errorf("Expected at least 3 symptom sections (one per issue), found %d", symptomCount)
	}
}

func TestTroubleshootingGuideExplainsHealthOutput(t *testing.T) {
	content, err := os.ReadFile(troubleshootingPath)
	if err != nil {
		t.Fatalf("Failed to read docs/guides/troubleshooting.md: %v", err)
	}

	text := string(content)

	// Check for /health endpoint explanation
	if !strings.Contains(text, "/health") && !strings.Contains(text, "health endpoint") {
		t.Error("Troubleshooting guide missing /health endpoint explanation")
	}

	// Check for sample JSON response
	if !strings.Contains(text, "sample") && !strings.Contains(text, "Sample") && !strings.Contains(text, "example") && !strings.Contains(text, "Example") {
		t.Error("Troubleshooting guide missing sample /health response")
	}

	// Check for key diagnostic fields explanation
	diagnosticFields := []string{
		"status",
		"healthy",
		"circuit_breaker",
		"consecutive_failures",
		"available_keys",
	}

	for _, field := range diagnosticFields {
		if !strings.Contains(text, field) {
			t.Errorf("Troubleshooting guide missing explanation of diagnostic field: %s", field)
		}
	}

	// Check for diagnosis scenarios
	if !strings.Contains(text, "Scenario") && !strings.Contains(text, "scenario") {
		t.Error("Troubleshooting guide missing diagnosis scenarios")
	}
}
