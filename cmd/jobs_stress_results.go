package cmd

import (
	"io"
	"os"
	"sort"
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
	backendCounts := make(map[string]int)
	allLatencies := make([]time.Duration, 0)

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

		// Aggregate backend counts and latencies
		for _, result := range r.response.Results {
			if result.Backend != "" {
				backendCounts[result.Backend]++
			}
			if result.DurationMs > 0 {
				allLatencies = append(allLatencies, time.Duration(result.DurationMs)*time.Millisecond)
			}
		}
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

	// Latency percentiles
	if len(allLatencies) > 0 {
		fmt.Println(styles.Header.Render("Latency (across all requests)"))
		sort.Slice(allLatencies, func(i, j int) bool {
			return allLatencies[i] < allLatencies[j]
		})

		var total time.Duration
		for _, l := range allLatencies {
			total += l
		}
		avg := total / time.Duration(len(allLatencies))

		p50Idx := len(allLatencies) / 2
		p95Idx := int(float64(len(allLatencies)) * 0.95)
		p99Idx := int(float64(len(allLatencies)) * 0.99)

		// Ensure valid indices
		if p95Idx >= len(allLatencies) {
			p95Idx = len(allLatencies) - 1
		}
		if p99Idx >= len(allLatencies) {
			p99Idx = len(allLatencies) - 1
		}

		fmt.Printf("  Min:     %s\n", allLatencies[0].Round(time.Millisecond))
		fmt.Printf("  Average: %s\n", avg.Round(time.Millisecond))
		fmt.Printf("  P50:     %s\n", allLatencies[p50Idx].Round(time.Millisecond))
		fmt.Printf("  P95:     %s\n", allLatencies[p95Idx].Round(time.Millisecond))
		fmt.Printf("  P99:     %s\n", allLatencies[p99Idx].Round(time.Millisecond))
		fmt.Printf("  Max:     %s\n", allLatencies[len(allLatencies)-1].Round(time.Millisecond))
		fmt.Println()
	}

	// Backend distribution
	if len(backendCounts) > 0 {
		fmt.Println(styles.Header.Render("Backend Distribution"))

		backends := make([]string, 0, len(backendCounts))
		for b := range backendCounts {
			backends = append(backends, b)
		}
		sort.Strings(backends)

		for _, backend := range backends {
			count := backendCounts[backend]
			pct := float64(count) / float64(totalCompleted) * 100
			fmt.Printf("  %-15s %4d (%5.1f%%)\n", backend, count, pct)
		}
		fmt.Println()
	}
}
