package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dotcommander/llambo/providers"
)

func runRouteQuery(cmd *commandIO, args []string) error {
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	cfg.Routing.ApplyDefaults()
	routingConfigs, err := routingProviderConfigs(cfg.Providers, cfg.Routing)
	if err != nil {
		return fmt.Errorf("prepare routing providers: %w", err)
	}

	metricsStore, err := providers.NewRoutingMetricsStore(cfg.Routing.MetricsPath)
	if err != nil {
		return fmt.Errorf("load routing metrics: %w", err)
	}

	preferencesPath, err := routePreferencesPath()
	if err != nil {
		return err
	}
	preferences, err := loadRoutePreferences(preferencesPath)
	if err != nil {
		return err
	}

	result, err := routeQuery(args[0], routingConfigs, cfg.Routing, metricsStore.Snapshot(), preferences)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "intent: %s\n", result.Intent)
	fmt.Fprintf(cmd.OutOrStdout(), "model: %s/%s\n", result.Provider, result.Model)
	if result.Reason != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "reason: %s\n", result.Reason)
	}
	return nil
}

func (cliOpts *invocationOptions) runRouteSimulate(cmd *commandIO, args []string) error {
	out := cmd.OutOrStdout()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	cfg.Routing.ApplyDefaults()
	routingConfigs, err := routingProviderConfigs(cfg.Providers, cfg.Routing)
	if err != nil {
		return fmt.Errorf("prepare routing providers: %w", err)
	}
	eventsPath := cliOpts.routeEventsPath
	if strings.TrimSpace(eventsPath) == "" {
		eventsPath = cfg.Routing.EventsPath
	}

	events, err := providers.ReadRouteEvents(eventsPath)
	if err != nil {
		return fmt.Errorf("read events: %w", err)
	}
	if len(events) == 0 {
		return fmt.Errorf("no route events found in %s", eventsPath)
	}

	if cliOpts.routeLimit > 0 && cliOpts.routeLimit < len(events) {
		events = events[len(events)-cliOpts.routeLimit:]
	}

	metricsStore, err := providers.NewRoutingMetricsStore(cfg.Routing.MetricsPath)
	if err != nil {
		return fmt.Errorf("load routing metrics: %w", err)
	}
	metrics := metricsStore.Snapshot()

	modes := splitCSV(cliOpts.routeModes)
	if len(modes) == 0 {
		return fmt.Errorf("no --modes provided")
	}

	fmt.Fprintf(out, "Replay source: %s\n", eventsPath)
	fmt.Fprintf(out, "Events replayed: %d\n\n", len(events))

	printActualBaselineTo(out, events)
	fmt.Fprintln(out)

	reports := make([]modeReport, 0, len(modes))
	for _, mode := range modes {
		routing := cfg.Routing
		routing.Mode = mode

		stats := simulateMode(routingConfigs, routing, metrics, events)
		fmt.Fprintf(out, "Mode: %s\n", mode)
		fmt.Fprintf(out, "  Decisions: %d\n", stats.decisions)
		fmt.Fprintf(out, "  Different from actual chosen provider: %d (%.1f%%)\n", stats.diffCount, pct(stats.diffCount, stats.decisions))
		fmt.Fprintf(out, "  Estimated avg latency: %dms\n", avgInt64(stats.totalLatencyMs, stats.decisions))
		fmt.Fprintf(out, "  Estimated avg cost: $%.6f\n", avgFloat(stats.totalCost, stats.decisions))
		fmt.Fprintf(out, "  Provider distribution: %s\n\n", formatProviderCounts(stats.providerCounts, stats.decisions))
		if len(stats.sampleReasons) > 0 {
			fmt.Fprintln(out, "  Sample reasons:")
			for _, reason := range stats.sampleReasons {
				fmt.Fprintf(out, "    - %s\n", reason)
			}
			fmt.Fprintln(out)
		}

		reports = append(reports, modeReport{Mode: mode, Stats: stats})
	}

	if strings.TrimSpace(cliOpts.routeReportPath) != "" {
		baseline := calculateActualBaseline(events)
		content := buildRouteSimulationReport(cliOpts.routeReportPath, eventsPath, len(events), baseline, reports)
		if err := os.WriteFile(cliOpts.routeReportPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
		fmt.Fprintf(out, "Report written to %s\n", cliOpts.routeReportPath)
	}

	return nil
}

type actualBaseline struct {
	decisions      int
	totalLatencyMs int64
	totalCost      float64
	providerCounts map[string]int
}

type modeReport struct {
	Mode  string
	Stats simulationStats
}

type simulationStats struct {
	decisions      int
	diffCount      int
	totalLatencyMs int64
	totalCost      float64
	providerCounts map[string]int
	sampleReasons  []string
}

func simulateMode(configs map[string]providers.Config, routing providers.RoutingConfig, metrics map[string]providers.ProviderRuntimeMetrics, events []providers.RouteEvent) simulationStats {
	stats := simulationStats{providerCounts: make(map[string]int)}
	for _, ev := range events {
		decision := providers.SimulateRoute(configs, routing, metrics, ev.Intent, ev.EstimatedTokens)
		if decision.Chosen == "" {
			continue
		}
		stats.decisions++
		stats.providerCounts[decision.Chosen]++
		if ev.ChosenProvider != "" && ev.ChosenProvider != decision.Chosen {
			stats.diffCount++
		}
		cfg := configs[decision.Chosen]
		if decision.Reason != "" && len(stats.sampleReasons) < 3 {
			stats.sampleReasons = append(stats.sampleReasons, fmt.Sprintf("%s: %s", routeDecisionLabel(decision.Chosen, cfg), decision.Reason))
		}
		stats.totalLatencyMs += providers.EstimateProviderLatencyMs(decision.Chosen, cfg, metrics)
		stats.totalCost += providers.EstimateProviderCostUSD(cfg, ev.EstimatedTokens)
	}
	return stats
}

func routeDecisionLabel(provider string, cfg providers.Config) string {
	if strings.Contains(provider, ":") || strings.TrimSpace(cfg.Model) == "" {
		return provider
	}
	return provider + "/" + cfg.Model
}

func printActualBaseline(events []providers.RouteEvent) {
	printActualBaselineTo(os.Stdout, events)
}

func printActualBaselineTo(out io.Writer, events []providers.RouteEvent) {
	b := calculateActualBaseline(events)

	fmt.Fprintln(out, "Actual baseline (from recorded events):")
	fmt.Fprintf(out, "  Decisions: %d\n", b.decisions)
	fmt.Fprintf(out, "  Avg latency: %dms\n", avgInt64(b.totalLatencyMs, b.decisions))
	fmt.Fprintf(out, "  Avg cost: $%.6f\n", avgFloat(b.totalCost, b.decisions))
	fmt.Fprintf(out, "  Provider distribution: %s\n", formatProviderCounts(b.providerCounts, b.decisions))
}

func calculateActualBaseline(events []providers.RouteEvent) actualBaseline {
	counts := make(map[string]int)
	var totalLatency int64
	var totalCost float64
	considered := 0
	for _, ev := range events {
		if ev.ChosenProvider == "" {
			continue
		}
		counts[ev.ChosenProvider]++
		if ev.LatencyMs > 0 {
			totalLatency += ev.LatencyMs
		}
		totalCost += ev.CostUSD
		considered++
	}
	return actualBaseline{
		decisions:      considered,
		totalLatencyMs: totalLatency,
		totalCost:      totalCost,
		providerCounts: counts,
	}
}

func buildRouteSimulationReport(reportPath, sourcePath string, events int, baseline actualBaseline, reports []modeReport) string {
	var b strings.Builder
	b.WriteString("# Routing Simulation Report\n\n")
	b.WriteString(fmt.Sprintf("- Generated: %s\n", time.Now().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- Source: `%s`\n", sourcePath))
	b.WriteString(fmt.Sprintf("- Events replayed: %d\n", events))
	b.WriteString(fmt.Sprintf("- Output: `%s`\n\n", reportPath))

	b.WriteString("## Actual Baseline\n\n")
	b.WriteString(fmt.Sprintf("- Decisions: %d\n", baseline.decisions))
	b.WriteString(fmt.Sprintf("- Avg latency: %dms\n", avgInt64(baseline.totalLatencyMs, baseline.decisions)))
	b.WriteString(fmt.Sprintf("- Avg cost: $%.6f\n", avgFloat(baseline.totalCost, baseline.decisions)))
	b.WriteString(fmt.Sprintf("- Provider distribution: %s\n\n", formatProviderCounts(baseline.providerCounts, baseline.decisions)))

	b.WriteString("## Mode Comparison\n\n")
	b.WriteString("| Mode | Decisions | Diff vs Actual | Est Avg Latency | Est Avg Cost | Provider Distribution | Sample Reason |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | --- | --- |\n")
	for _, r := range reports {
		b.WriteString(fmt.Sprintf("| %s | %d | %d (%.1f%%) | %dms | $%.6f | %s | %s |\n",
			r.Mode,
			r.Stats.decisions,
			r.Stats.diffCount,
			pct(r.Stats.diffCount, r.Stats.decisions),
			avgInt64(r.Stats.totalLatencyMs, r.Stats.decisions),
			avgFloat(r.Stats.totalCost, r.Stats.decisions),
			formatProviderCounts(r.Stats.providerCounts, r.Stats.decisions),
			markdownTableCell(sampleReason(r.Stats.sampleReasons)),
		))
	}

	return b.String()
}

// Scalar helpers retain their signatures with independent default options.
func runRouteSimulate(cmd *commandIO, args []string) error {
	return defaultInvocationOptions().runRouteSimulate(cmd, args)
}
