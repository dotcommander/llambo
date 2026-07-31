package api

import (
	"os"
	"strings"
	"testing"
)

const jobsDocPath = "../jobs.md"

func TestJobsDocExists(t *testing.T) {
	_, err := os.Stat(jobsDocPath)
	if err != nil {
		t.Fatalf("docs/api/jobs.md does not exist: %v", err)
	}
}

func TestJobsDocHasAllRequiredEndpoints(t *testing.T) {
	content, err := os.ReadFile(jobsDocPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/jobs.md: %v", err)
	}

	text := string(content)

	// Check for all three required endpoints
	requiredEndpoints := []string{
		"POST /v1/jobs",
		"GET /v1/jobs/{id}",
		"POST /v1/jobs/{id}/cancel",
	}

	for _, endpoint := range requiredEndpoints {
		if !strings.Contains(text, endpoint) {
			t.Errorf("jobs.md missing endpoint: %s", endpoint)
		}
	}
}

func TestJobsDocHasRequestSchemaWithSystemPromptAndRequestsArray(t *testing.T) {
	content, err := os.ReadFile(jobsDocPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/jobs.md: %v", err)
	}

	text := string(content)

	// Check for CreateJobRequest schema table
	if !strings.Contains(text, "CreateJobRequest") {
		t.Error("jobs.md does not contain CreateJobRequest schema")
	}

	// Check for system_prompt field in the schema
	if !strings.Contains(text, "system_prompt") {
		t.Error("jobs.md does not document system_prompt field in request schema")
	}

	// Check for requests array field in the schema
	if !strings.Contains(text, "requests") {
		t.Error("jobs.md does not document requests array field in request schema")
	}

	// Check for JobRequest object documentation
	if !strings.Contains(text, "JobRequest") {
		t.Error("jobs.md does not document JobRequest object")
	}

	// Check for Message object documentation (should be referenced)
	if !strings.Contains(text, "Message") {
		t.Error("jobs.md does not reference Message object")
	}
}

func TestJobsDocHasAllJobStatusValues(t *testing.T) {
	content, err := os.ReadFile(jobsDocPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/jobs.md: %v", err)
	}

	text := string(content)

	// Check for all job status values
	requiredStatuses := []string{
		"pending",
		"processing",
		"completed",
		"failed",
		"partial_failure",
		"cancelled",
	}

	for _, status := range requiredStatuses {
		if !strings.Contains(text, status) {
			t.Errorf("jobs.md missing job status value: %s", status)
		}
	}

	// Check for job status table or section explaining status values
	// Look for a table or list that mentions status values
	lines := strings.Split(text, "\n")
	foundStatusSection := false
	for _, line := range lines {
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "job status") && (strings.Contains(lowerLine, "values") || strings.Contains(lowerLine, "description")) {
			foundStatusSection = true
			break
		}
	}

	if !foundStatusSection {
		t.Error("jobs.md does not have a dedicated job status values section")
	}
}

func TestJobsDocHasCompleteExamples(t *testing.T) {
	content, err := os.ReadFile(jobsDocPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/jobs.md: %v", err)
	}

	text := string(content)

	// Check for curl examples for all three endpoints
	if !strings.Contains(text, "curl -X POST http://localhost:8080/v1/jobs") {
		t.Error("jobs.md missing POST /v1/jobs example")
	}

	if !strings.Contains(text, "curl http://localhost:8080/v1/jobs/job-") {
		t.Error("jobs.md missing GET /v1/jobs/{id} example")
	}

	if !strings.Contains(text, "curl -X POST http://localhost:8080/v1/jobs/job-") && strings.Contains(text, "/cancel") {
		t.Error("jobs.md missing POST /v1/jobs/{id}/cancel example")
	}
}

func TestJobsDocHasResponseExamplesWithAllStatuses(t *testing.T) {
	content, err := os.ReadFile(jobsDocPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/jobs.md: %v", err)
	}

	text := string(content)

	// Check for response examples with different statuses
	// Look for JSON examples containing status fields
	if !strings.Contains(text, "\"status\": \"pending\"") {
		t.Error("jobs.md missing pending status response example")
	}

	if !strings.Contains(text, "\"status\": \"processing\"") {
		t.Error("jobs.md missing processing status response example")
	}

	if !strings.Contains(text, "\"status\": \"completed\"") {
		t.Error("jobs.md missing completed status response example")
	}

	if !strings.Contains(text, "\"status\": \"partial_failure\"") {
		t.Error("jobs.md missing partial_failure status response example")
	}
}

func TestJobsDocStructure(t *testing.T) {
	content, err := os.ReadFile(jobsDocPath)
	if err != nil {
		t.Fatalf("Failed to read docs/api/jobs.md: %v", err)
	}

	text := string(content)

	// Check for proper markdown structure
	requiredSections := []string{
		"# Batch Jobs",
		"## Overview",
		"## Endpoints",
		"## Request Body Schema",
		"## Examples",
		"## Response",
		"## Job Status Values",
		"## Behavior Notes",
		"## Common Use Cases",
		"## Integration Notes",
	}

	for _, section := range requiredSections {
		if !strings.Contains(text, section) {
			t.Errorf("jobs.md missing section: %s", section)
		}
	}
}
