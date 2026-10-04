package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/gateway"
	"github.com/dotcommander/llambo/internal/styles"
)

const maxJobStatusErrorBodyBytes = 1 << 20

// Run command flags

// Stress command flags

// JobStats holds aggregated statistics for job results
type JobStats struct {
	TotalRequests   int
	Completed       int
	Failed          int
	TotalDuration   time.Duration
	Latencies       []time.Duration
	BackendCounts   map[string]int
	IntegrityIssues []string
}

func (cliOpts *invocationOptions) runJobsRun(cmd *commandIO, args []string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s Submitting batch job with %d requests\n",
		styles.Info.Render(">>"),
		cliOpts.runCount)

	// Build requests
	requests := buildRequests(cliOpts.runCount, cliOpts.runPrompt)

	// Submit job
	jobResp, err := submitJobContext(cmd.Context(), cliOpts.runServer, requests, cliOpts.runSystem)
	if err != nil {
		return fmt.Errorf("submit job: %w", err)
	}

	fmt.Fprintf(out, "%s Job submitted: %s\n",
		styles.Success.Render(">>"),
		styles.Header.Render(jobResp.JobID))

	if !cliOpts.runWait {
		fmt.Fprintf(out, "Poll status: curl %s/v1/jobs/%s\n", cliOpts.runServer, jobResp.JobID)
		return nil
	}

	// Wait for completion
	start := time.Now()
	finalResp, err := waitForJobWithWriterContext(cmd.Context(), out, cliOpts.runServer, jobResp.JobID, cliOpts.runPoll, cliOpts.runTimeout)
	if err != nil {
		return fmt.Errorf("wait for job: %w", err)
	}
	totalDuration := time.Since(start)

	// Calculate and print stats
	stats := calculateStats(finalResp, requests, cliOpts.runVerify)
	stats.TotalDuration = totalDuration

	cliOpts.printJobResultsTo(out, finalResp, stats)

	return nil
}

func (cliOpts *invocationOptions) runJobsStress(cmd *commandIO, args []string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s Starting stress test: %d jobs x %d requests = %d total requests\n",
		styles.Info.Render(">>"),
		cliOpts.stressJobs,
		cliOpts.stressRequestsPerJob,
		cliOpts.stressJobs*cliOpts.stressRequestsPerJob)

	start := time.Now()

	jobIndexes := make([]int, cliOpts.stressJobs)
	for i := range jobIndexes {
		jobIndexes[i] = i
	}

	results := runOrderedParallel(jobIndexes, func(_ int, jobIndex int) stressJobResult {
		jobStart := time.Now()

		requests := make([]gateway.JobRequest, cliOpts.stressRequestsPerJob)
		for j := 0; j < cliOpts.stressRequestsPerJob; j++ {
			n := jobIndex*cliOpts.stressRequestsPerJob + j + 1
			prompt := strings.ReplaceAll(cliOpts.stressPrompt, "{n}", fmt.Sprintf("%d", n))
			requests[j] = gateway.JobRequest{
				ID:       fmt.Sprintf("job%d-req-%03d", jobIndex+1, j+1),
				Messages: []gateway.Message{{Role: "user", Content: prompt}},
			}
		}

		jobResp, err := submitJobContext(cmd.Context(), cliOpts.stressServer, requests, "")
		if err != nil {
			return stressJobResult{err: err}
		}

		finalResp, err := waitForJobContext(cmd.Context(), cliOpts.stressServer, jobResp.JobID, 200*time.Millisecond, 5*time.Minute)
		return stressJobResult{
			jobID:    jobResp.JobID,
			response: finalResp,
			err:      err,
			duration: time.Since(jobStart),
		}
	})
	totalDuration := time.Since(start)
	if err := cmd.Context().Err(); err != nil {
		return err
	}

	// Aggregate results
	printStressResultsTo(out, results, totalDuration, cliOpts.stressJobs*cliOpts.stressRequestsPerJob)

	return nil
}

func buildRequests(count int, promptTemplate string) []gateway.JobRequest {
	requests := make([]gateway.JobRequest, count)
	for i := 0; i < count; i++ {
		n := i + 1
		prompt := strings.ReplaceAll(promptTemplate, "{n}", fmt.Sprintf("%d", n))
		requests[i] = gateway.JobRequest{
			ID:       fmt.Sprintf("req-%03d", n),
			Messages: []gateway.Message{{Role: "user", Content: prompt}},
		}
	}
	return requests
}

func submitJob(serverURL string, requests []gateway.JobRequest, systemPrompt string) (*gateway.JobResponse, error) {
	return submitJobContext(context.Background(), serverURL, requests, systemPrompt)
}

func submitJobContext(ctx context.Context, serverURL string, requests []gateway.JobRequest, systemPrompt string) (*gateway.JobResponse, error) {
	reqBody := gateway.CreateJobRequest{
		Requests:     requests,
		SystemPrompt: systemPrompt,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/v1/jobs", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create post request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB cap on error bodies
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var jobResp gateway.JobResponse
	if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &jobResp, nil
}

func waitForJob(serverURL, jobID string, pollInterval, timeout time.Duration) (*gateway.JobResponse, error) {
	return waitForJobContext(context.Background(), serverURL, jobID, pollInterval, timeout)
}

func waitForJobContext(ctx context.Context, serverURL, jobID string, pollInterval, timeout time.Duration) (*gateway.JobResponse, error) {
	return waitForJobWithWriterContext(ctx, os.Stdout, serverURL, jobID, pollInterval, timeout)
}

func waitForJobWithWriter(out io.Writer, serverURL, jobID string, pollInterval, timeout time.Duration) (*gateway.JobResponse, error) {
	return waitForJobWithWriterContext(context.Background(), out, serverURL, jobID, pollInterval, timeout)
}

func waitForJobWithWriterContext(ctx context.Context, out io.Writer, serverURL, jobID string, pollInterval, timeout time.Duration) (*gateway.JobResponse, error) {
	deadline := time.Now().Add(timeout)
	lastCompleted := -1

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/v1/jobs/"+jobID, nil)
		if err != nil {
			return nil, fmt.Errorf("create job status request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("get job status: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxJobStatusErrorBodyBytes))
			_ = resp.Body.Close()
			if readErr != nil {
				return nil, fmt.Errorf("read job status response (HTTP %d): %w", resp.StatusCode, readErr)
			}
			return nil, fmt.Errorf("unexpected job status HTTP %d: %s", resp.StatusCode, string(body))
		}

		var jobResp gateway.JobResponse
		if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()

		// Print progress if changed
		if jobResp.Completed != lastCompleted {
			printProgressTo(out, &jobResp)
			lastCompleted = jobResp.Completed
		}

		// Check if done
		if jobResp.Status == "completed" || jobResp.Status == "failed" || jobResp.Status == "cancelled" {
			return &jobResp, nil
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	return nil, fmt.Errorf("timeout after %v", timeout)
}

func printProgress(resp *gateway.JobResponse) {
	printProgressTo(os.Stdout, resp)
}

func printProgressTo(out io.Writer, resp *gateway.JobResponse) {
	pending := resp.Total - resp.Completed - resp.Failed
	status := "Processing"
	if resp.Status == "pending" {
		status = "Pending"
	}

	fmt.Fprintf(out, "\r%s [%d/%d] %s... (%d completed, %d failed, %d pending)     ",
		styles.Dim.Render(">>"),
		resp.Completed+resp.Failed,
		resp.Total,
		status,
		resp.Completed,
		resp.Failed,
		pending)
}

// Scalar helpers retain their signatures with independent default options.
func runJobsStress(cmd *commandIO, args []string) error {
	return defaultInvocationOptions().runJobsStress(cmd, args)
}

func runJobsRun(cmd *commandIO, args []string) error {
	return defaultInvocationOptions().runJobsRun(cmd, args)
}
