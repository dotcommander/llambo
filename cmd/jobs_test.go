package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/gateway"
)

func TestWaitForJobRejectsNonOKStatusImmediatelyWithBoundedDiagnostic(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(strings.Repeat("x", maxJobStatusErrorBodyBytes+1024)))
	}))
	t.Cleanup(server.Close)

	_, err := waitForJobWithWriterContext(context.Background(), &bytes.Buffer{}, server.URL, "job-1", time.Millisecond, time.Second)
	if err == nil || !strings.Contains(err.Error(), "unexpected job status HTTP 503") {
		t.Fatalf("waitForJobWithWriterContext() error = %v, want HTTP 503 diagnostic", err)
	}
	if len(err.Error()) > maxJobStatusErrorBodyBytes+128 {
		t.Fatalf("diagnostic length = %d, want at most %d", len(err.Error()), maxJobStatusErrorBodyBytes+128)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestWaitForJobPreservesSuccessfulTerminalResponse(t *testing.T) {
	t.Parallel()

	want := gateway.JobResponse{JobID: "job-1", Status: "completed", Total: 2, Completed: 2}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(want); err != nil {
			t.Errorf("Encode() error = %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var out bytes.Buffer
	got, err := waitForJobWithWriterContext(context.Background(), &out, server.URL, want.JobID, time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("waitForJobWithWriterContext() error = %v", err)
	}
	if got.JobID != want.JobID || got.Status != want.Status || got.Total != want.Total || got.Completed != want.Completed {
		t.Fatalf("response = %+v, want %+v", got, want)
	}
	if !strings.Contains(out.String(), "2 completed") {
		t.Fatalf("progress output = %q, want completed count", out.String())
	}
}
