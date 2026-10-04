package cmd

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/styles"
)

// Ping result rendering. All display logic for `llambo ping` lives here;
// ping execution stays in ping_output.go. Colors come from internal/styles
// (lipgloss) and are stripped automatically for non-TTY output and NO_COLOR.

const (
	pingPreviewMaxRunes = 48
	pingErrorMaxRunes   = 200
	pingModelColMax     = 40
	pingProviderColMax  = 14
)

// pingLatencyBucket classifies latency for coloring: 0 fast, 1 normal, 2 slow.
func pingLatencyBucket(d time.Duration) int {
	switch {
	case d < time.Second:
		return 0
	case d < 3*time.Second:
		return 1
	default:
		return 2
	}
}

func pingStyleForLatency(d time.Duration) string {
	switch pingLatencyBucket(d) {
	case 0:
		return styles.Success.Render(fmt.Sprintf("%7dms", d.Milliseconds()))
	case 2:
		return styles.Warning.Render(fmt.Sprintf("%7dms", d.Milliseconds()))
	default:
		return fmt.Sprintf("%7dms", d.Milliseconds())
	}
}

// pingDisplayWidths computes provider/model column widths from the result set,
// capped so pathological names cannot explode the table.
func pingDisplayWidths(results []PingResult) (providerW, modelW int) {
	providerW, modelW = len("PROVIDER"), len("MODEL")
	for _, r := range results {
		if n := len(r.Provider); n > providerW {
			providerW = n
		}
		if n := len(r.Model); n > modelW {
			modelW = n
		}
	}
	return min(providerW, pingProviderColMax), min(modelW, pingModelColMax)
}

func pingTruncateASCII(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func pingTruncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// pingCollapseError flattens a multi-line provider error body into one
// scannable line.
func pingCollapseError(err string) string {
	s := strings.Join(strings.Fields(err), " ")
	return pingTruncateRunes(s, pingErrorMaxRunes)
}

func pingCostLabel(r PingResult) string {
	label := catalog.PriceLabel(catalog.CostStatus(r.CostStatus), r.InputCostPer1M, r.OutputCostPer1M)
	if r.CostStatus != string(catalog.CostPaid) || (r.TokensIn == 0 && r.TokensOut == 0) {
		return label
	}
	input := float64(r.TokensIn) * r.InputCostPer1M / 1_000_000
	output := float64(r.TokensOut) * r.OutputCostPer1M / 1_000_000
	return fmt.Sprintf("%s, actual $%.6f", label, input+output)
}

func pingCostBracket(r PingResult) string {
	label := pingCostLabel(r)
	if label == "" {
		return ""
	}
	bracket := "[" + label + "]"
	if strings.HasPrefix(label, string(catalog.CostFree)) {
		return styles.Success.Render(bracket)
	}
	return styles.Dim.Render(bracket)
}

// printResultsTable prints the full ping table: header, rule, and one row per
// result (results are expected pre-sorted by latency, fastest first).
func printResultsTable(out io.Writer, results []PingResult) {
	providerW, modelW := pingDisplayWidths(results)
	fmt.Fprintf(out, "%s %s %s %s  %s\n",
		" ",
		styles.Header.Render(fmt.Sprintf("%-*s", providerW, "PROVIDER")),
		styles.Header.Render(fmt.Sprintf("%-*s", modelW, "MODEL")),
		styles.Header.Render(fmt.Sprintf("%8s", "LATENCY")),
		styles.Header.Render("RESULT"))
	fmt.Fprintln(out, styles.Dim.Render(strings.Repeat("─", providerW+modelW+26)))
	for _, r := range results {
		printResultRow(out, r, providerW, modelW)
	}
}

// printResult prints a single result row with fixed legacy widths. Retained
// for callers without a table context (models discover-free).
func printResult(out io.Writer, r PingResult) {
	printResultRow(out, r, 12, 35)
}

func printResultRow(out io.Writer, r PingResult, providerW, modelW int) {
	status := styles.Success.Render("✓")
	if !r.Success {
		status = styles.Error.Render("✗")
	}

	fmt.Fprintf(out, "%s %-*s %-*s %s",
		status, providerW, r.Provider, modelW, pingTruncateASCII(r.Model, modelW),
		pingStyleForLatency(r.Latency))

	if r.Success {
		resp := pingTruncateRunes(strings.ReplaceAll(r.Response, "\n", " "), pingPreviewMaxRunes)
		fmt.Fprintf(out, "  %s", styles.Dim.Render(fmt.Sprintf("%q", resp)))
		if r.TokensIn > 0 || r.TokensOut > 0 {
			fmt.Fprintf(out, " %s", styles.Dim.Render(fmt.Sprintf("[%d→%d tok]", r.TokensIn, r.TokensOut)))
		}
	} else {
		fmt.Fprintf(out, "  %s", styles.Error.Render("FAIL"))
	}
	if bracket := pingCostBracket(r); bracket != "" {
		fmt.Fprintf(out, " %s", bracket)
	}
	fmt.Fprintln(out)
}

// pingPercentile returns the p-quantile of an ascending-sorted slice.
func pingPercentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[min(max(idx, 0), len(sorted)-1)]
}

// pingTotalSpend sums the actual cost of successful paid results.
func pingTotalSpend(results []PingResult) float64 {
	var total float64
	for _, r := range results {
		if !r.Success || r.CostStatus != string(catalog.CostPaid) || (r.TokensIn == 0 && r.TokensOut == 0) {
			continue
		}
		total += float64(r.TokensIn) * r.InputCostPer1M / 1_000_000
		total += float64(r.TokensOut) * r.OutputCostPer1M / 1_000_000
	}
	return total
}

func printSummary(out io.Writer, results []PingResult) {
	sectionRule := styles.Dim.Render(strings.Repeat("─", 60))

	fmt.Fprintln(out, "\n"+sectionRule)
	fmt.Fprintln(out, styles.Header.Render("SUMMARY"))
	fmt.Fprintln(out, sectionRule)

	var successful, failed int
	var totalLatency time.Duration
	var latencies []time.Duration
	var fastest PingResult
	var fastestSeen bool

	for _, r := range results {
		if !r.Success {
			failed++
			continue
		}
		successful++
		totalLatency += r.Latency
		latencies = append(latencies, r.Latency)
		if !fastestSeen || r.Latency < fastest.Latency {
			fastest = r
			fastestSeen = true
		}
	}

	fmt.Fprintf(out, "Targets: %d  %s  %s\n", len(results),
		styles.Success.Render(fmt.Sprintf("✓ %d", successful)),
		styles.Error.Render(fmt.Sprintf("✗ %d", failed)))

	if successful > 0 {
		fmt.Fprintf(out, "Latency: avg %dms  p50 %dms  p95 %dms\n",
			(totalLatency / time.Duration(successful)).Milliseconds(),
			pingPercentile(latencies, 0.50).Milliseconds(),
			pingPercentile(latencies, 0.95).Milliseconds())
		fmt.Fprintf(out, "Fastest: %s (%dms)\n",
			styles.Info.Render(fastest.Provider+"/"+fastest.Model),
			fastest.Latency.Milliseconds())
		fmt.Fprintf(out, "Total spend: $%.5f\n", pingTotalSpend(results))
	}

	// Inefficiency analysis
	fmt.Fprintln(out, "\n"+sectionRule)
	fmt.Fprintln(out, styles.Header.Render("INEFFICIENCY ANALYSIS"))
	fmt.Fprintln(out, sectionRule)

	for _, r := range results {
		if !r.Success {
			fmt.Fprintf(out, "%s %s: %s\n",
				styles.Warning.Render("⚠"),
				pingTruncateASCII(r.Provider+"/"+r.Model, pingProviderColMax+pingModelColMax),
				pingStyledError(r.Error))
			continue
		}

		var issues []string

		// Slow response (>5s)
		if r.Latency > 5*time.Second {
			issues = append(issues, fmt.Sprintf("slow response (%dms)", r.Latency.Milliseconds()))
		}

		// No token reporting
		if r.TokensIn == 0 && r.TokensOut == 0 {
			issues = append(issues, "no token usage reported")
		}

		// Empty response
		if strings.TrimSpace(r.Response) == "" {
			issues = append(issues, "empty response")
		}

		if len(issues) > 0 {
			fmt.Fprintf(out, "%s %s: %s\n",
				styles.Warning.Render("⚠"),
				pingTruncateASCII(r.Provider+"/"+r.Model, pingProviderColMax+pingModelColMax),
				styles.Warning.Render(strings.Join(issues, ", ")))
		}
	}
}

// pingStyledError colors the error classification prefix and dims the body.
func pingStyledError(err string) string {
	collapsed := pingCollapseError(err)
	if prefix, body, ok := strings.Cut(collapsed, ": "); ok {
		return styles.Error.Render(prefix+":") + " " + styles.Dim.Render(body)
	}
	return styles.Error.Render(collapsed)
}
