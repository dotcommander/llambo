package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/gateway"
	"github.com/dotcommander/llambo/internal/styles"
)

// Repeated string literals consolidated by goconst.
const (
	failed1 = "failed"
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

func (cliOpts *invocationOptions) printJobResultsTo(out io.Writer, resp *gateway.JobResponse, stats *JobStats) {
	printer := commandPrinter{out}
	printer.Println() // Clear progress line
	printer.Println()

	// Header
	printer.Println(strings.Repeat("─", 70))
	printer.Printf("%s\n", styles.Header.Render("JOB RESULTS"))
	printer.Println(strings.Repeat("─", 70))

	// Status
	statusStyle := styles.Success
	switch resp.Status {
	case failed1:
		statusStyle = styles.Error
	case "cancelled":
		statusStyle = styles.Warning
	}
	printer.Printf("Job ID:   %s\n", resp.JobID)
	printer.Printf("Status:   %s\n", statusStyle.Render(resp.Status))
	printer.Println()

	// Summary stats
	printer.Println(styles.Header.Render("Summary"))
	printer.Printf("  Total requests:    %d\n", stats.TotalRequests)
	printer.Printf("  Completed:         %s\n", styles.Success.Render(printer.Sprintf("%d", stats.Completed)))
	if stats.Failed > 0 {
		printer.Printf("  Failed:            %s\n", styles.Error.Render(printer.Sprintf("%d", stats.Failed)))
	} else {
		printer.Printf("  Failed:            %d\n", stats.Failed)
	}
	printer.Printf("  Total duration:    %s\n", stats.TotalDuration.Round(time.Millisecond))

	// Throughput
	if stats.TotalDuration > 0 {
		rps := float64(stats.TotalRequests) / stats.TotalDuration.Seconds()
		printer.Printf("  Requests/second:   %.2f\n", rps)
	}

	// Success rate
	if stats.TotalRequests > 0 {
		successRate := float64(stats.Completed) / float64(stats.TotalRequests) * 100
		printer.Printf("  Success rate:      %.1f%%\n", successRate)
	}
	printer.Println()

	printRequestLatencyStats(printer, "Latency", stats.Latencies)
	printRequestBackendDistribution(printer, stats.BackendCounts, stats.TotalRequests)

	// Integrity verification
	if cliOpts.runVerify {
		printer.Println(styles.Header.Render("Integrity Verification"))
		if len(stats.IntegrityIssues) == 0 {
			printer.Printf("  %s All request IDs accounted for\n", styles.Success.Render("PASS"))
			printer.Printf("  %s No duplicate IDs\n", styles.Success.Render("PASS"))
			printer.Printf("  %s Result count matches request count\n", styles.Success.Render("PASS"))
		} else {
			for _, issue := range stats.IntegrityIssues {
				printer.Printf("  %s %s\n", styles.Error.Render("FAIL"), issue)
			}
		}
		printer.Println()
	}

	// Show failed requests if any
	if stats.Failed > 0 {
		printer.Println(styles.Header.Render("Failed Requests"))
		for _, result := range resp.Results {
			if result.Status == failed1 {
				printer.Printf("  %s: %s\n",
					styles.Dim.Render(result.ID),
					styles.Error.Render(result.Error))
			}
		}
		printer.Println()
	}
}

// stressJobResult holds the result of a single stress test job
