package gateway

import (
	"testing"
	"time"
)

func TestJob_ToResponse(t *testing.T) {
	t.Parallel()
	now := time.Now()
	job := &Job{
		ID:        "job-123",
		Status:    JobStatusCompleted,
		Total:     2,
		Completed: 2,
		Failed:    0,
		Results: []JobResult{
			{ID: "req-1", Status: string(ResultStatusCompleted), Content: "response 1"},
			{ID: "req-2", Status: string(ResultStatusCompleted), Content: "response 2"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	resp := job.ToResponse()

	if resp.JobID != "job-123" {
		t.Errorf("expected JobID %q, got %q", "job-123", resp.JobID)
	}
	if resp.Status != string(JobStatusCompleted) {
		t.Errorf("expected Status %q, got %q", JobStatusCompleted, resp.Status)
	}
	if resp.Total != 2 {
		t.Errorf("expected Total=2, got %d", resp.Total)
	}
	if resp.Completed != 2 {
		t.Errorf("expected Completed=2, got %d", resp.Completed)
	}
	if resp.Failed != 0 {
		t.Errorf("expected Failed=0, got %d", resp.Failed)
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.CreatedAt != now.Unix() {
		t.Errorf("expected CreatedAt=%d, got %d", now.Unix(), resp.CreatedAt)
	}
	if resp.UpdatedAt != now.Unix() {
		t.Errorf("expected UpdatedAt=%d, got %d", now.Unix(), resp.UpdatedAt)
	}
}
