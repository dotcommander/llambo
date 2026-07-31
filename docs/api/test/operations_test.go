package api

import (
	"os"
	"strings"
	"testing"
)

func TestOperationsDocumentationExists(t *testing.T) {
	// Acceptance criterion 1: Given docs/api/operations.md exists, when read,
	// then all three operational endpoints are documented
	content, err := os.ReadFile("../../api/operations.md")
	if err != nil {
		t.Fatalf("docs/api/operations.md does not exist: %v", err)
	}

	doc := string(content)

	// Check that all three endpoints are documented
	endpoints := []string{
		"## Health Check",
		"## Providers",
		"## Stats",
	}

	for _, endpoint := range endpoints {
		if !strings.Contains(doc, endpoint) {
			t.Errorf("Missing documentation for endpoint: %s", endpoint)
		}
	}

	t.Log("✓ All three operational endpoints are documented in operations.md")
}

func TestHealthEndpointDocumentation(t *testing.T) {
	// Acceptance criterion 2: Given the documentation, when checking /health,
	// then backend status format and circuit breaker states are explained
	content, err := os.ReadFile("../../api/operations.md")
	if err != nil {
		t.Fatalf("Failed to read operations.md: %v", err)
	}

	doc := string(content)

	// Find health check section
	healthStart := strings.Index(doc, "## Health Check")
	if healthStart == -1 {
		t.Fatal("Health Check section not found")
	}

	// Extract health section (up to next top-level ## or end of file)
	healthSection := doc[healthStart:]
	// Find next occurrence of "\n## " (newline followed by ## and space)
	// Starting from position 1 to skip the current ##
	if nextSection := strings.Index(healthSection[1:], "\n## "); nextSection != -1 {
		healthSection = healthSection[:nextSection+1]
	}

	// Check for required backend status fields
	requiredFields := []string{
		"healthy",
		"failures",
		"disabled_at",
		"uptime_seconds",
	}

	for _, field := range requiredFields {
		if !strings.Contains(strings.ToLower(healthSection), strings.ToLower(field)) {
			t.Errorf("Health documentation missing required field: %s", field)
		}
	}

	// Check for circuit breaker explanation
	circuitBreakerTerms := []string{
		"circuit breaker",
		"Rate limit errors",
		"cooldown",
		"auto-recover",
	}

	foundCount := 0
	for _, term := range circuitBreakerTerms {
		if strings.Contains(strings.ToLower(healthSection), strings.ToLower(term)) {
			foundCount++
		}
	}

	if foundCount < 2 {
		t.Errorf("Health documentation should explain circuit breaker states. Found %d/4 relevant terms", foundCount)
	}

	// Check for status values explanation
	if !strings.Contains(healthSection, "healthy") || !strings.Contains(healthSection, "degraded") {
		t.Errorf("Health documentation should explain status values (healthy, degraded)")
	}

	t.Log("✓ Health endpoint documentation includes backend status format and circuit breaker states")
}

func TestStatsEndpointDocumentation(t *testing.T) {
	// Acceptance criterion 3: Given the docs, when checking /stats,
	// then token usage and cost fields are documented
	content, err := os.ReadFile("../../api/operations.md")
	if err != nil {
		t.Fatalf("Failed to read operations.md: %v", err)
	}

	doc := string(content)

	// Find stats section
	statsStart := strings.Index(doc, "## Stats")
	if statsStart == -1 {
		t.Fatal("Stats section not found")
	}

	// Extract stats section (up to next top-level ## or end of file)
	statsSection := doc[statsStart:]
	// Find next occurrence of "\n## " (newline followed by ## and space)
	// Starting from position 1 to skip the current ##
	if nextSection := strings.Index(statsSection[1:], "\n## "); nextSection != -1 {
		statsSection = statsSection[:nextSection+1]
	}

	// Check for token usage fields
	tokenFields := []string{
		"prompt_tokens",
		"completion_tokens",
		"total_tokens",
	}

	for _, field := range tokenFields {
		if !strings.Contains(strings.ToLower(statsSection), strings.ToLower(field)) {
			t.Errorf("Stats documentation missing token field: %s", field)
		}
	}

	// Check for cost fields
	costFields := []string{
		"total_cost_usd",
		"cost",
		"USD",
	}

	foundCostField := false
	for _, field := range costFields {
		if strings.Contains(strings.ToLower(statsSection), strings.ToLower(field)) {
			foundCostField = true
			break
		}
	}

	if !foundCostField {
		t.Error("Stats documentation should include cost fields")
	}

	// Check for per-provider and total aggregation
	if !strings.Contains(statsSection, "providers") || !strings.Contains(statsSection, "total") {
		t.Error("Stats documentation should show both per-provider and total aggregation")
	}

	// Check for requests count
	if !strings.Contains(statsSection, "requests") {
		t.Error("Stats documentation should include requests count")
	}

	t.Log("✓ Stats endpoint documentation includes token usage and cost fields")
}

func TestOperationsMdReferencedInReadme(t *testing.T) {
	// Additional test: operations.md should be referenced in README
	content, err := os.ReadFile("../../api/README.md")
	if err != nil {
		t.Fatalf("Failed to read README.md: %v", err)
	}

	doc := string(content)

	if !strings.Contains(doc, "[Operational Endpoints](operations.md)") {
		t.Error("operations.md should be referenced in README.md Available Documentation section")
	}

	// Check quick reference links
	if !strings.Contains(doc, "operations.md#health-check") ||
		!strings.Contains(doc, "operations.md#providers") ||
		!strings.Contains(doc, "operations.md#stats") {
		t.Error("README.md quick reference should link to operations.md sections")
	}

	t.Log("✓ operations.md properly referenced in README.md")
}

func TestApiMdReferencesOperations(t *testing.T) {
	// Additional test: api.md should reference operations.md instead of duplicating
	content, err := os.ReadFile("../../api/api.md")
	if err != nil {
		t.Fatalf("Failed to read api.md: %v", err)
	}

	doc := string(content)

	// Check that api.md references operations.md for health, providers, stats
	references := []string{
		"operations.md#health-check",
		"operations.md#providers",
		"operations.md#stats",
	}

	for _, ref := range references {
		if !strings.Contains(doc, ref) {
			t.Errorf("api.md should reference %s", ref)
		}
	}

	// Check that old detailed sections are removed (just references remain)
	if strings.Count(doc, "## Health Check") > 1 ||
		strings.Count(doc, "## Providers") > 1 ||
		strings.Count(doc, "## Stats") > 1 {
		t.Error("api.md should not duplicate detailed documentation (should only reference operations.md)")
	}

	t.Log("✓ api.md properly references operations.md without duplication")
}
