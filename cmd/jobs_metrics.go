package cmd

import (
	"sort"
	"time"

	"github.com/dotcommander/llambo/internal/gateway"
	"github.com/dotcommander/llambo/internal/styles"
)

// jobRequestMetrics collects result-level metrics shared by batch and stress reports.
type jobRequestMetrics struct {
	backendCounts map[string]int
	latencies     []time.Duration
}

func newJobRequestMetrics() jobRequestMetrics {
	return jobRequestMetrics{
		backendCounts: make(map[string]int),
		latencies:     make([]time.Duration, 0),
	}
}

func (m *jobRequestMetrics) add(result gateway.JobResult) {
	if result.Backend != "" {
		m.backendCounts[result.Backend]++
	}
	if result.DurationMs > 0 {
		m.latencies = append(m.latencies, time.Duration(result.DurationMs)*time.Millisecond)
	}
}

func (m *jobRequestMetrics) addAll(results []gateway.JobResult) {
	for _, result := range results {
		m.add(result)
	}
}

func printRequestLatencyStats(fmt commandPrinter, heading string, latencies []time.Duration) {
	if len(latencies) == 0 {
		return
	}

	fmt.Println(styles.Header.Render(heading))
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	var total time.Duration
	for _, latency := range latencies {
		total += latency
	}
	avg := total / time.Duration(len(latencies))
	n := len(latencies)
	p95Idx := int(float64(n) * 0.95)
	if p95Idx >= n {
		p95Idx = n - 1
	}
	p99Idx := int(float64(n) * 0.99)
	if p99Idx >= n {
		p99Idx = n - 1
	}

	fmt.Printf("  Min:     %s\n", latencies[0].Round(time.Millisecond))
	fmt.Printf("  Average: %s\n", avg.Round(time.Millisecond))
	fmt.Printf("  P50:     %s\n", latencies[n/2].Round(time.Millisecond))
	fmt.Printf("  P95:     %s\n", latencies[p95Idx].Round(time.Millisecond))
	fmt.Printf("  P99:     %s\n", latencies[p99Idx].Round(time.Millisecond))
	fmt.Printf("  Max:     %s\n", latencies[n-1].Round(time.Millisecond))
	fmt.Println()
}

func printRequestBackendDistribution(fmt commandPrinter, backendCounts map[string]int, _ int) {
	if len(backendCounts) == 0 {
		return
	}

	total := 0
	for _, count := range backendCounts {
		total += count
	}
	fmt.Println(styles.Header.Render("Backend Distribution"))
	backends := make([]string, 0, len(backendCounts))
	for backend := range backendCounts {
		backends = append(backends, backend)
	}
	sort.Strings(backends)

	for _, backend := range backends {
		count := backendCounts[backend]
		percentage := pct(count, total)
		fmt.Printf("  %-15s %4d (%5.1f%%)\n", backend, count, percentage)
	}
	fmt.Println()
}
