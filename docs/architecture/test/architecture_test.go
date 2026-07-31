package architecture

import (
	"os"
	"strings"
	"testing"
)

const (
	readmePath  = "../README.md"
	archDocPath = "../architecture.md"
)

func TestArchitectureReadmeExists(t *testing.T) {
	_, err := os.Stat(readmePath)
	if err != nil {
		t.Fatalf("docs/architecture/README.md does not exist: %v", err)
	}
}

func TestArchitectureDocumentationExists(t *testing.T) {
	_, err := os.Stat(archDocPath)
	if err != nil {
		t.Fatalf("docs/architecture/architecture.md does not exist: %v", err)
	}
}

func TestReadmeHasSystemDiagram(t *testing.T) {
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read docs/architecture/README.md: %v", err)
	}

	text := string(content)

	// Check for system components section (text-based diagram)
	if !strings.Contains(text, "System Components") {
		t.Error("README.md does not contain 'System Components' section")
	}

	// Check for code block with directory structure (text-based diagram)
	lines := strings.Split(text, "\n")
	inCodeBlock := false
	hasCmdDirectory := false
	hasInternalDirectory := false
	hasInternalGateway := false
	hasProvidersDirectory := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Look for code block start
		if strings.HasPrefix(trimmed, "```") && !inCodeBlock {
			inCodeBlock = true
			continue
		}

		// Look for code block end
		if inCodeBlock && strings.HasPrefix(trimmed, "```") {
			inCodeBlock = false
			continue
		}

		// Check content within code block
		if inCodeBlock {
			if strings.Contains(trimmed, "cmd/") {
				hasCmdDirectory = true
			}
			// Check for internal/gateway/ either as combined string or on separate lines
			if strings.Contains(trimmed, "internal/gateway/") {
				hasInternalGateway = true
			}
			// Also check for internal/ followed by gateway/ on next line (indented format)
			if strings.Contains(trimmed, "internal/") {
				// Look ahead in the code block for gateway/ on next line
				// This is handled by tracking state
				hasInternalDirectory = true
			}
			if strings.Contains(trimmed, "gateway/") && hasInternalDirectory {
				hasInternalGateway = true
			}
			if strings.Contains(trimmed, "providers/") {
				hasProvidersDirectory = true
			}
		}
	}

	if !hasCmdDirectory {
		t.Error("System diagram should mention cmd/ directory")
	}
	if !hasInternalGateway {
		t.Error("System diagram should mention internal/gateway/ directory")
	}
	if !hasProvidersDirectory {
		t.Error("System diagram should mention providers/ directory")
	}
}

func TestArchitectureMdHasSystemDiagram(t *testing.T) {
	content, err := os.ReadFile(archDocPath)
	if err != nil {
		t.Fatalf("Failed to read docs/architecture/architecture.md: %v", err)
	}

	text := string(content)

	// Check for text-based ASCII diagram (look for box drawing characters or structured layout)
	// The diagram should have some visual structure
	hasBoxDrawing := false
	hasStructuredLayout := false

	// Check for common box drawing characters or visual indicators
	boxChars := []string{"┌", "┐", "└", "┘", "─", "│", "┬", "┴", "├", "┤", "┼", "╭", "╮", "╰", "╯"}

	for _, char := range boxChars {
		if strings.Contains(text, char) {
			hasBoxDrawing = true
			break
		}
	}

	// Check for structured layout with clear sections
	lines := strings.Split(text, "\n")
	sectionCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "┌") || strings.HasPrefix(trimmed, "╭") {
			sectionCount++
		}
	}

	if sectionCount >= 2 {
		hasStructuredLayout = true
	}

	// Accept either box drawing or structured layout
	if !hasBoxDrawing && !hasStructuredLayout {
		t.Error("architecture.md should contain a text-based system diagram with visual structure")
	}
}

func TestReadmeExplainsComponentPurposes(t *testing.T) {
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read docs/architecture/README.md: %v", err)
	}

	text := string(content)

	// Check that cmd/ purpose is explained (CLI layer)
	if !strings.Contains(text, "cmd/") && !strings.Contains(strings.ToLower(text), "cli") {
		t.Error("README.md should explain cmd/ directory purpose (CLI layer)")
	}

	// Check that internal/gateway/ purpose is explained
	if !strings.Contains(text, "internal/gateway/") && !strings.Contains(strings.ToLower(text), "gateway") {
		t.Error("README.md should explain internal/gateway/ directory purpose (gateway layer)")
	}

	// Check that providers/ purpose is explained
	if !strings.Contains(text, "providers/") && !strings.Contains(strings.ToLower(text), "provider") {
		t.Error("README.md should explain providers/ directory purpose (provider layer)")
	}
}

func TestArchitectureMdExplainsComponentPurposes(t *testing.T) {
	content, err := os.ReadFile(archDocPath)
	if err != nil {
		t.Fatalf("Failed to read docs/architecture/architecture.md: %v", err)
	}

	text := string(content)

	// Check for component details sections
	requiredSections := []string{
		"### CLI Layer (`cmd/`)",
		"### Gateway Layer (`internal/gateway/`)",
		"### Provider Layer (`providers/`)",
	}

	for _, section := range requiredSections {
		if !strings.Contains(text, section) {
			t.Errorf("architecture.md missing section: %s", section)
		}
	}

	// Check that each section explains the purpose
	lines := strings.Split(text, "\n")
	inCliSection := false
	inGatewaySection := false
	inProviderSection := false
	cliPurposeExplained := false
	gatewayPurposeExplained := false
	providerPurposeExplained := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track which section we're in
		if strings.HasPrefix(trimmed, "### CLI Layer (`cmd/`)") {
			inCliSection = true
			inGatewaySection = false
			inProviderSection = false
			continue
		}

		if strings.HasPrefix(trimmed, "### Gateway Layer (`internal/gateway/`)") {
			inCliSection = false
			inGatewaySection = true
			inProviderSection = false
			continue
		}

		if strings.HasPrefix(trimmed, "### Provider Layer (`providers/`)") {
			inCliSection = false
			inGatewaySection = false
			inProviderSection = true
			continue
		}

		// Check for next section header (h2 or h3)
		if strings.HasPrefix(trimmed, "## ") || (strings.HasPrefix(trimmed, "### ") && !strings.Contains(trimmed, "Layer")) {
			inCliSection = false
			inGatewaySection = false
			inProviderSection = false
		}

		// Look for purpose explanations in each section
		if inCliSection && len(trimmed) > 0 {
			// CLI purpose keywords
			if strings.Contains(strings.ToLower(trimmed), "cobra") ||
				strings.Contains(strings.ToLower(trimmed), "command") ||
				strings.Contains(strings.ToLower(trimmed), "cli") {
				cliPurposeExplained = true
			}
		}

		if inGatewaySection && len(trimmed) > 0 {
			// Gateway purpose keywords
			if strings.Contains(strings.ToLower(trimmed), "http") ||
				strings.Contains(strings.ToLower(trimmed), "server") ||
				strings.Contains(strings.ToLower(trimmed), "handler") ||
				strings.Contains(strings.ToLower(trimmed), "endpoint") {
				gatewayPurposeExplained = true
			}
		}

		if inProviderSection && len(trimmed) > 0 {
			// Provider purpose keywords
			if strings.Contains(strings.ToLower(trimmed), "openai") ||
				strings.Contains(strings.ToLower(trimmed), "backend") ||
				strings.Contains(strings.ToLower(trimmed), "api") ||
				strings.Contains(strings.ToLower(trimmed), "client") {
				providerPurposeExplained = true
			}
		}
	}

	if !cliPurposeExplained {
		t.Error("CLI Layer section should explain the purpose of cmd/ directory")
	}
	if !gatewayPurposeExplained {
		t.Error("Gateway Layer section should explain the purpose of internal/gateway/ directory")
	}
	if !providerPurposeExplained {
		t.Error("Provider Layer section should explain the purpose of providers/ directory")
	}
}

func TestRequestLifecycleDocumented(t *testing.T) {
	content, err := os.ReadFile(archDocPath)
	if err != nil {
		t.Fatalf("Failed to read docs/architecture/architecture.md: %v", err)
	}

	text := string(content)

	// Check for request flow section
	if !strings.Contains(text, "Request Flow") && !strings.Contains(text, "request flow") {
		t.Error("architecture.md should have a 'Request Flow' section")
	}

	// Check for HTTP → provider → response flow
	lines := strings.Split(text, "\n")
	hasHttpMention := false
	hasProviderMention := false
	hasResponseMention := false
	inRequestFlowSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Find request flow section
		if strings.HasPrefix(trimmed, "## Request Flow") ||
			strings.HasPrefix(trimmed, "### Request Flow") ||
			(strings.Contains(strings.ToLower(trimmed), "request flow") &&
				(strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### "))) {
			inRequestFlowSection = true
			continue
		}

		// Check for next major section
		if inRequestFlowSection && strings.HasPrefix(trimmed, "## ") &&
			!strings.Contains(strings.ToLower(trimmed), "request flow") {
			inRequestFlowSection = false
		}

		// Look for key terms in request flow section
		if inRequestFlowSection {
			lowerLine := strings.ToLower(trimmed)
			if strings.Contains(lowerLine, "http") || strings.Contains(lowerLine, "post /") {
				hasHttpMention = true
			}
			if strings.Contains(lowerLine, "provider") || strings.Contains(lowerLine, "backend") {
				hasProviderMention = true
			}
			if strings.Contains(lowerLine, "response") || strings.Contains(lowerLine, "result") {
				hasResponseMention = true
			}
		}
	}

	if !hasHttpMention {
		t.Error("Request flow should mention HTTP requests")
	}
	if !hasProviderMention {
		t.Error("Request flow should mention providers/backends")
	}
	if !hasResponseMention {
		t.Error("Request flow should mention responses/results")
	}

	// Check for specific flow documentation - look for diagram or step-by-step
	if !strings.Contains(text, "Single Chat Completion") && !strings.Contains(text, "Batch Job Processing") {
		t.Error("Request flow should document specific flows like 'Single Chat Completion' or 'Batch Job Processing'")
	}
}
