package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

// PingResult holds the result of a single provider benchmark
type PingResult struct {
	Provider        string            `json:"provider"`
	Model           string            `json:"model"`
	Success         bool              `json:"success"`
	Error           string            `json:"error,omitempty"`
	Latency         time.Duration     `json:"latency_ms"`
	TTFB            time.Duration     `json:"ttfb_ms,omitempty"` // time to first byte (if streaming)
	Response        string            `json:"response,omitempty"`
	TokensIn        int               `json:"tokens_in,omitempty"`
	TokensOut       int               `json:"tokens_out,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	RawResponse     json.RawMessage   `json:"raw_response,omitempty"`
	CostStatus      string            `json:"cost_status,omitempty"`
	InputCostPer1M  float64           `json:"input_cost_per_1m,omitempty"`
	OutputCostPer1M float64           `json:"output_cost_per_1m,omitempty"`
}

type pingTarget struct {
	Name       string
	Config     providers.Config
	CostStatus catalog.CostStatus
	InputCost  float64
	OutputCost float64
}

type pingRunContext struct {
	Prompt             string
	Targets            []pingTarget
	Routing            providers.RoutingConfig
	RecordRouteMetrics bool
}

var (
	pingPrompt             string
	pingOutput             string
	pingProviders          string
	pingModels             string
	pingIncludeQuarantine  bool
	pingFreeOnly           bool
	pingIncludeUnknownCost bool
	pingTimeout            int
	pingMaxOutputCost      float64
	pingRecordMetrics      bool
)

func runPing(cmd *commandIO, args []string) error {
	out := cmd.OutOrStdout()
	runCtx, err := preparePingRunWithIO(cmd)
	if err != nil {
		return err
	}

	printPingPlan(out, runCtx)

	results := executePingTargetsContext(cmd.Context(), runCtx.Targets, runCtx.Prompt)
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	if runCtx.RecordRouteMetrics {
		if err := recordPingRoutingMetrics(results, runCtx.Routing); err != nil {
			return err
		}
		fmt.Fprintf(out, "Routing metrics updated: %s\n", runCtx.Routing.MetricsPath)
	}
	if err := recordPingCatalogHealth(results); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
	}

	// Sort results by latency ascending (fastest first)
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Latency < results[j].Latency
	})

	fmt.Fprintf(out, "%-12s %-35s %8s  %s\n", "PROVIDER", "MODEL", "LATENCY", "RESULT")
	fmt.Fprintln(out, strings.Repeat("─", 90))
	for _, result := range results {
		printResult(out, result)
	}

	// Summary
	printSummary(out, results)

	// Write output if requested
	if pingOutput != "" {
		if err := writePingOutput(pingOutput, results); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nResults written to %s\n", pingOutput)
	}

	return nil
}

func preparePingRun() (pingRunContext, error) {
	return preparePingRunWithIO(&commandIO{ctx: context.Background(), stdout: io.Discard, stderr: io.Discard})
}

func preparePingRunWithIO(cmd *commandIO) (pingRunContext, error) {
	if err := validatePingFlags(); err != nil {
		return pingRunContext{}, err
	}

	runCtx := pingRunContext{Prompt: pingPrompt}

	targets, routing, err := loadPingTargetsWithIO(cmd)
	if err != nil {
		return pingRunContext{}, err
	}
	runCtx.Targets = targets
	runCtx.Routing = routing
	runCtx.RecordRouteMetrics = pingRecordMetrics

	return runCtx, nil
}

func validatePingFlags() error {
	if pingTimeout <= 0 {
		return fmt.Errorf("invalid --timeout-seconds %d (must be > 0)", pingTimeout)
	}

	return nil
}

func loadPingTargets() ([]pingTarget, providers.RoutingConfig, error) {
	return loadPingTargetsWithIO(&commandIO{ctx: context.Background(), stdout: io.Discard, stderr: io.Discard})
}

func loadPingTargetsWithIO(cmd *commandIO) ([]pingTarget, providers.RoutingConfig, error) {
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return nil, providers.RoutingConfig{}, fmt.Errorf("load config: %w", err)
	}
	cfg.Routing.ApplyDefaults()

	// Global cap from config applies when the per-call flag is unset (0).
	effectiveMaxOutputCost := pingMaxOutputCost
	effectiveIncludeUnknownCost := pingIncludeUnknownCost
	if effectiveMaxOutputCost == 0 {
		effectiveMaxOutputCost = cfg.MaxOutputCost
		if cfg.MaxOutputCost > 0 {
			effectiveIncludeUnknownCost = true
		}
	}

	costMap, err := costs.LoadAll()
	if err != nil {
		return nil, providers.RoutingConfig{}, fmt.Errorf("load model costs: %w", err)
	}

	if pingModels != "" || pingFreeOnly || pingMaxOutputCost > 0 {
		selector := pingModels
		if selector == "" && pingFreeOnly {
			selector = "free"
		}
		catPath, err := catalog.CatalogPath()
		if err != nil {
			return nil, providers.RoutingConfig{}, err
		}
		cat, err := catalog.Load(catPath)
		if err != nil {
			return nil, providers.RoutingConfig{}, fmt.Errorf("load catalog: %w", err)
		}
		selected, err := catalog.ResolveModels(cat, cfg.Providers, costMap, catalog.SelectorOptions{
			Selector:           selector,
			ProviderFilter:     pingProviders,
			IncludeQuarantine:  pingIncludeQuarantine,
			FreeOnly:           pingFreeOnly,
			IncludeUnknownCost: effectiveIncludeUnknownCost,
			MaxOutputCost:      effectiveMaxOutputCost,
			Blocklist:          providers.NewBlocklist(cfg.Blocklist),
		})
		if err != nil {
			return nil, providers.RoutingConfig{}, err
		}
		targets := make([]pingTarget, 0, len(selected))
		skippedNonChat := 0
		for _, target := range selected {
			if ok, reason := catalog.TextChatCapability(target.Provider, target.Model, modelEntryForTarget(cat, target.Provider, target.Model)); !ok {
				skippedNonChat++
				fmt.Fprintf(cmd.ErrOrStderr(), "Chat filter: skipped %s/%s (%s)\n", target.Provider, target.Model, reason)
				continue
			}
			targets = append(targets, pingTarget{
				Name:       target.Provider,
				Config:     target.Config,
				CostStatus: target.CostStatus,
				InputCost:  target.InputPer1M,
				OutputCost: target.OutputPer1M,
			})
		}
		if skippedNonChat > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Chat filter: skipped %d non-text chat target(s)\n", skippedNonChat)
		}
		if len(targets) == 0 {
			return nil, providers.RoutingConfig{}, fmt.Errorf("no text chat-capable models match selector %q", selector)
		}
		return targets, cfg.Routing, nil
	}

	cat, err := loadCatalogForPingFilter()
	if err != nil {
		return nil, providers.RoutingConfig{}, err
	}
	targets := buildPingTargetsWithWriter(cmd.ErrOrStderr(), cfg.Providers, costMap, providers.NewBlocklist(cfg.Blocklist), effectiveMaxOutputCost, effectiveIncludeUnknownCost, cat)
	if len(targets) == 0 {
		return nil, providers.RoutingConfig{}, fmt.Errorf("no enabled providers found")
	}

	targets = filterByProvider(targets, pingProviders)
	if pingProviders != "" && len(targets) == 0 {
		return nil, providers.RoutingConfig{}, fmt.Errorf("no targets match --provider %q", pingProviders)
	}

	return targets, cfg.Routing, nil
}
