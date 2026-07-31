package docs

import (
	"os"
	"strings"
	"testing"
)

func TestContributingGuideExists(t *testing.T) {
	// Test that CONTRIBUTING.md exists
	_, err := os.Stat("CONTRIBUTING.md")
	if err != nil {
		t.Errorf("CONTRIBUTING.md does not exist: %v", err)
	}
}

func TestContributingGuideHasDevelopmentSetup(t *testing.T) {
	// Read the CONTRIBUTING.md file
	content, err := os.ReadFile("CONTRIBUTING.md")
	if err != nil {
		t.Fatalf("Failed to read CONTRIBUTING.md: %v", err)
	}

	guide := string(content)

	// Test that development setup steps are documented
	setupKeywords := []string{
		"Development Setup",
		"Prerequisites",
		"Getting the Source",
		"Building and Running",
		"go build",
		"ln -sf",
		"go test",
	}

	for _, keyword := range setupKeywords {
		if !strings.Contains(guide, keyword) {
			t.Errorf("CONTRIBUTING.md missing development setup keyword: %s", keyword)
		}
	}

	// Test specific critical build command is present
	if !strings.Contains(guide, "go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo") {
		t.Error("CONTRIBUTING.md missing critical build and symlink command")
	}
}

func TestContributingGuideHasGoConventions(t *testing.T) {
	content, err := os.ReadFile("CONTRIBUTING.md")
	if err != nil {
		t.Fatalf("Failed to read CONTRIBUTING.md: %v", err)
	}

	guide := string(content)

	// Test that Go conventions and project patterns are referenced
	conventionKeywords := []string{
		"Go Conventions",
		"Code Organization",
		"Naming Conventions",
		"Error Handling",
		"Testing Patterns",
		"t.Run()",
		"Table-driven",
		"Circuit Breaker",
		"Key Rotation",
		"Parallel Processing",
	}

	for _, keyword := range conventionKeywords {
		if !strings.Contains(guide, keyword) {
			t.Errorf("CONTRIBUTING.md missing Go conventions keyword: %s", keyword)
		}
	}
}

func TestContributingGuideHasPRProcess(t *testing.T) {
	content, err := os.ReadFile("CONTRIBUTING.md")
	if err != nil {
		t.Fatalf("Failed to read CONTRIBUTING.md: %v", err)
	}

	guide := string(content)

	// Test that PR process and commit message format are documented
	prKeywords := []string{
		"Pull Request Process",
		"Branch Strategy",
		"Creating a Pull Request",
		"Commit Message Format",
		"Conventional Commits",
		"feat(",
		"fix(",
		"docs(",
		"test(",
		"PR Review Checklist",
		"Review Expectations",
	}

	for _, keyword := range prKeywords {
		if !strings.Contains(guide, keyword) {
			t.Errorf("CONTRIBUTING.md missing PR process keyword: %s", keyword)
		}
	}

	// Test that commit message examples from git history are referenced
	if !strings.Contains(guide, "feat(providers): add native Gemini API with client caching") &&
		!strings.Contains(guide, "fix(gateway): fix job status and add partial_failure") {
		t.Error("CONTRIBUTING.md missing examples of commit message format")
	}
}
