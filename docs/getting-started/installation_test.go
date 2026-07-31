package getting_started

import (
	"os"
	"strings"
	"testing"
)

const installationPath = "installation.md"

func TestInstallationGuideExists(t *testing.T) {
	_, err := os.Stat(installationPath)
	if err != nil {
		t.Fatalf("installation.md does not exist: %v", err)
	}
}

func TestPrerequisitesSectionFirst(t *testing.T) {
	content, err := os.ReadFile(installationPath)
	if err != nil {
		t.Fatalf("Failed to read installation.md: %v", err)
	}

	lines := strings.Split(string(content), "\n")

	// Find the "Prerequisites" section
	foundPrerequisites := false
	for i, line := range lines {
		if strings.Contains(line, "## Prerequisites") {
			foundPrerequisites = true

			// Check if it's the first major section after introduction
			// Count sections before "## Prerequisites"
			sectionCount := 0
			for j := range i {
				if strings.HasPrefix(lines[j], "## ") {
					sectionCount++
				}
			}

			if sectionCount > 0 {
				t.Errorf("Prerequisites section is not first. Found %d sections before it", sectionCount)
			}
			break
		}
	}

	if !foundPrerequisites {
		t.Error("Prerequisites section not found in installation.md")
	}
}

func TestPrerequisitesIncludeGoVersion(t *testing.T) {
	content, err := os.ReadFile(installationPath)
	if err != nil {
		t.Fatalf("Failed to read installation.md: %v", err)
	}

	// Check for the go.mod-backed Go version mention.
	if !strings.Contains(string(content), "Go 1.25.5") {
		t.Error("installation.md does not mention Go 1.25.5 or newer in prerequisites")
	}
}

func TestExactInstallationCommandsProvided(t *testing.T) {
	content, err := os.ReadFile(installationPath)
	if err != nil {
		t.Fatalf("Failed to read installation.md: %v", err)
	}

	text := string(content)

	// Check for specific installation commands
	requiredCommands := []string{
		"go install",
		"go build -o llambo .",
		"ln -sf $(pwd)/llambo ~/go/bin/llambo",
	}

	for _, cmd := range requiredCommands {
		if !strings.Contains(text, cmd) {
			t.Errorf("installation.md missing required command: %s", cmd)
		}
	}
}

func TestLinksToConfiguration(t *testing.T) {
	content, err := os.ReadFile(installationPath)
	if err != nil {
		t.Fatalf("Failed to read installation.md: %v", err)
	}

	text := string(content)

	// Check for link to configuration guide
	if !strings.Contains(text, "Configuration Guide") || !strings.Contains(text, "../guides/configuration.md") {
		t.Error("installation.md does not link to configuration guide")
	}

	// Optional: Check if it's in a "Next Steps" section
	if !strings.Contains(text, "Next Steps") {
		t.Error("installation.md does not have a 'Next Steps' section")
	}
}
