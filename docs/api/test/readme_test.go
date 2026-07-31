package api

import (
	"os"
	"strings"
	"testing"
)

const readmePath = "../README.md"

func TestApiReadmeExists(t *testing.T) {
	_, err := os.Stat(readmePath)
	if err != nil {
		t.Fatalf("api/README.md does not exist: %v", err)
	}
}

func TestApiReadmeHasEndpointTable(t *testing.T) {
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read api/README.md: %v", err)
	}

	text := string(content)

	// Check for markdown table pattern
	if !strings.Contains(text, "| Endpoint | Method | Description |") {
		t.Error("api/README.md does not contain endpoint table header")
	}

	// Check if table has rows with pipe separators
	lines := strings.Split(text, "\n")
	tableRows := 0
	inTable := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "| Endpoint | Method | Description |") {
			inTable = true
			continue
		}
		if inTable && strings.HasPrefix(trimmed, "|") && strings.Contains(trimmed, "|") {
			tableRows++
		}
		if inTable && !strings.HasPrefix(trimmed, "|") && trimmed != "" {
			inTable = false
		}
	}

	// Should have at least 9 endpoints (from CLAUDE.md)
	if tableRows < 9 {
		t.Errorf("Endpoint table has only %d rows, expected at least 9", tableRows)
	}
}

func TestApiReadmeListsAllEndpoints(t *testing.T) {
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read api/README.md: %v", err)
	}

	text := string(content)

	// Check for all endpoints from CLAUDE.md
	requiredEndpoints := []string{
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/models",
		"/v1/jobs",
		"/v1/jobs/{id}",
		"/v1/jobs/{id}/cancel",
		"/health",
		"/providers",
		"/stats",
	}

	for _, endpoint := range requiredEndpoints {
		if !strings.Contains(text, endpoint) {
			t.Errorf("api/README.md missing endpoint: %s", endpoint)
		}
	}
}

func TestApiReadmeHasLinksToDetailedDocumentation(t *testing.T) {
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read api/README.md: %v", err)
	}

	text := string(content)

	// Should link to api.md (detailed documentation)
	if !strings.Contains(text, "[API Reference](api.md)") {
		t.Error("api/README.md does not link to detailed API documentation (api.md)")
	}

	// Check that endpoints in table have links to api.md sections
	// All endpoints except root should have links
	endpointsThatShouldHaveLinks := []string{
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/models",
		"/v1/jobs",
		"/health",
		"/providers",
		"/stats",
	}

	for _, endpoint := range endpointsThatShouldHaveLinks {
		// Look for markdown link pattern: [`endpoint`](filename)
		// The backticks are part of the link text
		// Special handling for chat completions which may link to separate file
		if endpoint == "/v1/chat/completions" {
			// Check for link to either api.md#chat-completions or chat-completions.md
			apiLinkPattern := "[`" + endpoint + "`](api.md#"
			separateLinkPattern := "[`" + endpoint + "`](chat-completions.md"
			if !strings.Contains(text, apiLinkPattern) && !strings.Contains(text, separateLinkPattern) {
				t.Errorf("Endpoint %s in table should have link to api.md section or chat-completions.md", endpoint)
			}
		} else if endpoint == "/v1/embeddings" {
			// Check for link to either api.md#embeddings or embeddings.md
			apiLinkPattern := "[`" + endpoint + "`](api.md#"
			separateLinkPattern := "[`" + endpoint + "`](embeddings.md"
			if !strings.Contains(text, apiLinkPattern) && !strings.Contains(text, separateLinkPattern) {
				t.Errorf("Endpoint %s in table should have link to api.md section or embeddings.md", endpoint)
			}
		} else if endpoint == "/health" || endpoint == "/providers" || endpoint == "/stats" {
			// Health, providers, stats should link to either api.md or operations.md
			apiLinkPattern := "[`" + endpoint + "`](api.md#"
			opsLinkPattern := "[`" + endpoint + "`](operations.md#"
			if !strings.Contains(text, apiLinkPattern) && !strings.Contains(text, opsLinkPattern) {
				t.Errorf("Endpoint %s in table should have link to api.md or operations.md section", endpoint)
			}
		} else {
			// Other endpoints should link to api.md sections
			linkPattern := "[`" + endpoint + "`](api.md#"
			if !strings.Contains(text, linkPattern) {
				t.Errorf("Endpoint %s in table should have link to api.md section", endpoint)
			}
		}
	}
}

func TestApiReadmeTableHasCorrectColumns(t *testing.T) {
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read api/README.md: %v", err)
	}

	lines := strings.Split(string(content), "\n")

	// Find the table
	for i, line := range lines {
		if strings.Contains(line, "| Endpoint | Method | Description |") {
			// Check table format - should have 3 columns
			headerParts := strings.Split(line, "|")
			if len(headerParts) != 5 { // Empty strings at start and end
				t.Errorf("Table header doesn't have 3 columns. Found %d parts", len(headerParts)-2)
			}

			// Check a few rows after header
			for j := i + 1; j < len(lines) && j < i+10; j++ {
				rowLine := strings.TrimSpace(lines[j])
				if rowLine == "" || !strings.HasPrefix(rowLine, "|") {
					break
				}
				rowParts := strings.Split(rowLine, "|")
				if len(rowParts) != 5 {
					t.Errorf("Table row doesn't have 3 columns. Found %d parts in row: %s", len(rowParts)-2, rowLine)
				}
			}
			break
		}
	}
}
