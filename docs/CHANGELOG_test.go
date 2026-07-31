package docs

import (
	"os"
	"strings"
	"testing"
)

func TestChangelogExists(t *testing.T) {
	// Test that CHANGELOG.md exists
	_, err := os.Stat("CHANGELOG.md")
	if err != nil {
		t.Errorf("CHANGELOG.md does not exist: %v", err)
	}
}

func TestChangelogFollowsKeepAChangelogFormat(t *testing.T) {
	// Read the CHANGELOG.md file
	content, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatalf("Failed to read CHANGELOG.md: %v", err)
	}

	changelog := string(content)

	// Test that Keep a Changelog format is followed
	requiredSections := []string{
		"# Changelog",
		"## [Unreleased]",
		"### Added",
		"### Changed",
		"### Fixed",
		"### Removed",
		"## [",
	}

	for _, section := range requiredSections {
		if !strings.Contains(changelog, section) {
			t.Errorf("CHANGELOG.md missing required section: %s", section)
		}
	}

	// Test that changelog mentions Keep a Changelog
	if !strings.Contains(changelog, "Keep a Changelog") {
		t.Error("CHANGELOG.md missing reference to Keep a Changelog format")
	}

	// Test that changelog mentions Semantic Versioning
	if !strings.Contains(changelog, "Semantic Versioning") {
		t.Error("CHANGELOG.md missing reference to Semantic Versioning")
	}
}

func TestChangelogHasUnreleasedSection(t *testing.T) {
	content, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatalf("Failed to read CHANGELOG.md: %v", err)
	}

	changelog := string(content)

	// Test that [Unreleased] is intentionally empty for the initial public release.
	if !strings.Contains(changelog, "## [Unreleased]") {
		t.Error("CHANGELOG.md missing [Unreleased] section header")
	}

	// Find the [Unreleased] section
	unreleasedIndex := strings.Index(changelog, "## [Unreleased]")
	if unreleasedIndex == -1 {
		t.Fatal("Could not find [Unreleased] section")
	}

	// Find the next section after [Unreleased]
	nextSectionIndex := strings.Index(changelog[unreleasedIndex+len("## [Unreleased]"):], "## [")

	var unreleasedSection string
	if nextSectionIndex != -1 {
		unreleasedSection = changelog[unreleasedIndex : unreleasedIndex+len("## [Unreleased]")+nextSectionIndex]
	} else {
		// If no next section, take from [Unreleased] to end
		unreleasedSection = changelog[unreleasedIndex:]
	}

	if strings.TrimSpace(unreleasedSection) != "## [Unreleased]" {
		t.Errorf("[Unreleased] must be empty before the next release:\n%s", unreleasedSection)
	}
}

func TestChangelogTypesOfChangesSection(t *testing.T) {
	content, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatalf("Failed to read CHANGELOG.md: %v", err)
	}

	changelog := string(content)

	// Test that "Types of Changes" section exists with all required change types
	requiredChangeTypes := []string{
		"**Added** for new features",
		"**Changed** for changes in existing functionality",
		"**Fixed** for any bug fixes",
		"**Removed** for now removed features",
	}

	for _, changeType := range requiredChangeTypes {
		if !strings.Contains(changelog, changeType) {
			t.Errorf("CHANGELOG.md missing change type definition: %s", changeType)
		}
	}
}
