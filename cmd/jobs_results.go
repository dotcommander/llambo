package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/gateway"
	"github.com/dotcommander/llambo/internal/styles"
)

func calculateStats(resp *gateway.JobResponse, originalRequests []gateway.JobRequest, verify bool) *JobStats {
	metrics := newJobRequestMetrics()
	stats := &JobStats{
		TotalRequests: resp.Total,
		Completed:     resp.Completed,
		Failed:        resp.Failed,
		BackendCounts: metrics.backendCounts,
		Latencies:     metrics.latencies,
	}

	// Build map of original request IDs
	requestIDs := make(map[string]bool)
	for _, req := range originalRequests {
		requestIDs[req.ID] = true
	}

	// Track seen IDs for integrity check
	seenIDs := make(map[string]bool)

	for _, result := range resp.Results {
		metrics.add(result)

		// Integrity checks
		if verify {
			if seenIDs[result.ID] {
				stats.IntegrityIssues = append(stats.IntegrityIssues, fmt.Sprintf("duplicate ID: %s", result.ID))
			}
			seenIDs[result.ID] = true

			if !requestIDs[result.ID] {
				stats.IntegrityIssues = append(stats.IntegrityIssues, fmt.Sprintf("unknown ID in results: %s", result.ID))
			}
		}
	}
	stats.Latencies = metrics.latencies

	// Check for missing IDs
	if verify {
		for id := range requestIDs {
			if !seenIDs[id] {
				stats.IntegrityIssues = append(stats.IntegrityIssues, fmt.Sprintf("missing ID in results: %s", id))
			}
		}

		if len(resp.Results) != len(originalRequests) {
			stats.IntegrityIssues = append(stats.IntegrityIssues,
				fmt.Sprintf("result count mismatch: expected %d, got %d", len(originalRequests), len(resp.Results)))
		}
	}

	return stats
}

func printJobResults(resp *gateway.JobResponse, stats *JobStats) {
	printJobResultsTo(os.Stdout, resp, stats)
}

func printJobResultsTo(out io.Writer, resp *gateway.JobResponse, stats *JobStats) {
	fmt := commandPrinter{out}
	fmt.Println() // Clear progress line
	fmt.Println()

	// Header
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("%s\n", styles.Header.Render("JOB RESULTS"))
	fmt.Println(strings.Repeat("─", 70))

	// Status
	statusStyle := styles.Success
	if resp.Status == "failed" {
		statusStyle = styles.Error
	} else if resp.Status == "cancelled" {
		statusStyle = styles.Warning
	}
	fmt.Printf("Job ID:   %s\n", resp.JobID)
	fmt.Printf("Status:   %s\n", statusStyle.Render(resp.Status))
	fmt.Println()

	// Summary stats
	fmt.Println(styles.Header.Render("Summary"))
	fmt.Printf("  Total requests:    %d\n", stats.TotalRequests)
	fmt.Printf("  Completed:         %s\n", styles.Success.Render(fmt.Sprintf("%d", stats.Completed)))
	if stats.Failed > 0 {
		fmt.Printf("  Failed:            %s\n", styles.Error.Render(fmt.Sprintf("%d", stats.Failed)))
	} else {
		fmt.Printf("  Failed:            %d\n", stats.Failed)
	}
	fmt.Printf("  Total duration:    %s\n", stats.TotalDuration.Round(time.Millisecond))

	// Throughput
	if stats.TotalDuration > 0 {
		rps := float64(stats.TotalRequests) / stats.TotalDuration.Seconds()
		fmt.Printf("  Requests/second:   %.2f\n", rps)
	}

	// Success rate
	if stats.TotalRequests > 0 {
		successRate := float64(stats.Completed) / float64(stats.TotalRequests) * 100
		fmt.Printf("  Success rate:      %.1f%%\n", successRate)
	}
	fmt.Println()

	printRequestLatencyStats(fmt, "Latency", stats.Latencies)
	printRequestBackendDistribution(fmt, stats.BackendCounts, stats.TotalRequests)

	// Integrity verification
	if runVerify {
		fmt.Println(styles.Header.Render("Integrity Verification"))
		if len(stats.IntegrityIssues) == 0 {
			fmt.Printf("  %s All request IDs accounted for\n", styles.Success.Render("PASS"))
			fmt.Printf("  %s No duplicate IDs\n", styles.Success.Render("PASS"))
			fmt.Printf("  %s Result count matches request count\n", styles.Success.Render("PASS"))
		} else {
			for _, issue := range stats.IntegrityIssues {
				fmt.Printf("  %s %s\n", styles.Error.Render("FAIL"), issue)
			}
		}
		fmt.Println()
	}

	// Show failed requests if any
	if stats.Failed > 0 {
		fmt.Println(styles.Header.Render("Failed Requests"))
		for _, result := range resp.Results {
			if result.Status == "failed" {
				fmt.Printf("  %s: %s\n",
					styles.Dim.Render(result.ID),
					styles.Error.Render(result.Error))
			}
		}
		fmt.Println()
	}
}

// stressJobResult holds the result of a single stress test job
