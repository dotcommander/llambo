package cmd

import (
	"io"
	"os"
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

func printStressResults(results []stressJobResult, totalDuration time.Duration, totalRequests int) {
	printStressResultsTo(os.Stdout, results, totalDuration, totalRequests)
}

func printStressResultsTo(out io.Writer, results []stressJobResult, totalDuration time.Duration, totalRequests int) {
	fmt := commandPrinter{out}
	fmt.Println()
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("%s\n", styles.Header.Render("STRESS TEST RESULTS"))
	fmt.Println(strings.Repeat("─", 70))

	// Per-job summary
	fmt.Println(styles.Header.Render("Per-Job Summary"))
	fmt.Printf("%-12s %-10s %8s %8s %12s\n", "JOB", "STATUS", "DONE", "FAILED", "DURATION")
	fmt.Println(strings.Repeat("─", 55))

	var totalCompleted, totalFailed int
	var successfulJobs int
	metrics := newJobRequestMetrics()

	for i, r := range results {
		jobName := fmt.Sprintf("job-%d", i+1)

		if r.err != nil {
			fmt.Printf("%-12s %s %8s %8s %12s\n",
				jobName,
				styles.Error.Render("ERROR"),
				"-",
				"-",
				r.err.Error())
			continue
		}

		statusStyle := styles.Success
		if r.response.Status == "failed" {
			statusStyle = styles.Error
		}

		fmt.Printf("%-12s %s %8d %8d %12s\n",
			jobName,
			statusStyle.Render(fmt.Sprintf("%-10s", r.response.Status)),
			r.response.Completed,
			r.response.Failed,
			r.duration.Round(time.Millisecond))

		totalCompleted += r.response.Completed
		totalFailed += r.response.Failed
		successfulJobs++

		metrics.addAll(r.response.Results)
	}

	fmt.Println()

	// Aggregate stats
	fmt.Println(styles.Header.Render("Aggregate Statistics"))
	fmt.Printf("  Total jobs:        %d (%d successful)\n", len(results), successfulJobs)
	fmt.Printf("  Total requests:    %d\n", totalRequests)
	fmt.Printf("  Completed:         %s\n", styles.Success.Render(fmt.Sprintf("%d", totalCompleted)))
	if totalFailed > 0 {
		fmt.Printf("  Failed:            %s\n", styles.Error.Render(fmt.Sprintf("%d", totalFailed)))
	} else {
		fmt.Printf("  Failed:            %d\n", totalFailed)
	}
	fmt.Printf("  Total duration:    %s\n", totalDuration.Round(time.Millisecond))

	// Throughput
	if totalDuration > 0 {
		rps := float64(totalCompleted) / totalDuration.Seconds()
		fmt.Printf("  Requests/second:   %.2f\n", rps)
	}

	// Success rate
	if totalRequests > 0 {
		successRate := float64(totalCompleted) / float64(totalRequests) * 100
		fmt.Printf("  Success rate:      %.1f%%\n", successRate)
	}
	fmt.Println()

	printRequestLatencyStats(fmt, "Latency (across all requests)", metrics.latencies)
	printRequestBackendDistribution(fmt, metrics.backendCounts, totalCompleted)
}
