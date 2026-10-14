package cmd

import (
	"io"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/gateway"
	"github.com/dotcommander/llambo/internal/styles"
)

type stressJobResult struct {
	jobID    string
	response *gateway.JobResponse
	err      error
	duration time.Duration
}

func printStressResultsTo(out io.Writer, results []stressJobResult, totalDuration time.Duration, totalRequests int) {
	printer := commandPrinter{out}
	printer.Println()
	printer.Println(strings.Repeat("─", 70))
	printer.Printf("%s\n", styles.Header.Render("STRESS TEST RESULTS"))
	printer.Println(strings.Repeat("─", 70))

	// Per-job summary
	printer.Println(styles.Header.Render("Per-Job Summary"))
	printer.Printf("%-12s %-10s %8s %8s %12s\n", "JOB", "STATUS", "DONE", "FAILED", "DURATION")
	printer.Println(strings.Repeat("─", 55))

	var totalCompleted, totalFailed int
	var successfulJobs int
	metrics := newJobRequestMetrics()

	for i, r := range results {
		jobName := printer.Sprintf("job-%d", i+1)

		if r.err != nil {
			printer.Printf("%-12s %s %8s %8s %12s\n",
				jobName,
				styles.Error.Render("ERROR"),
				"-",
				"-",
				r.err.Error())
			continue
		}

		statusStyle := styles.Success
		if r.response.Status == failed1 {
			statusStyle = styles.Error
		}

		printer.Printf("%-12s %s %8d %8d %12s\n",
			jobName,
			statusStyle.Render(printer.Sprintf("%-10s", r.response.Status)),
			r.response.Completed,
			r.response.Failed,
			r.duration.Round(time.Millisecond))

		totalCompleted += r.response.Completed
		totalFailed += r.response.Failed
		successfulJobs++

		metrics.addAll(r.response.Results)
	}

	printer.Println()

	// Aggregate stats
	printer.Println(styles.Header.Render("Aggregate Statistics"))
	printer.Printf("  Total jobs:        %d (%d successful)\n", len(results), successfulJobs)
	printer.Printf("  Total requests:    %d\n", totalRequests)
	printer.Printf("  Completed:         %s\n", styles.Success.Render(printer.Sprintf("%d", totalCompleted)))
	if totalFailed > 0 {
		printer.Printf("  Failed:            %s\n", styles.Error.Render(printer.Sprintf("%d", totalFailed)))
	} else {
		printer.Printf("  Failed:            %d\n", totalFailed)
	}
	printer.Printf("  Total duration:    %s\n", totalDuration.Round(time.Millisecond))

	// Throughput
	if totalDuration > 0 {
		rps := float64(totalCompleted) / totalDuration.Seconds()
		printer.Printf("  Requests/second:   %.2f\n", rps)
	}

	// Success rate
	if totalRequests > 0 {
		successRate := float64(totalCompleted) / float64(totalRequests) * 100
		printer.Printf("  Success rate:      %.1f%%\n", successRate)
	}
	printer.Println()

	printRequestLatencyStats(printer, "Latency (across all requests)", metrics.latencies)
	printRequestBackendDistribution(printer, metrics.backendCounts, totalCompleted)
}
