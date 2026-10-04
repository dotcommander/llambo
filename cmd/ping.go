package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
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
	Generation      time.Duration     `json:"generation_duration_ms,omitempty"`
	SpeedTokensPS   float64           `json:"speed_tokens_per_second,omitempty"`
	Response        string            `json:"response,omitempty"`
	TokensIn        int               `json:"tokens_in,omitempty"`
	TokensOut       int               `json:"tokens_out,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	RawResponse     json.RawMessage   `json:"raw_response,omitempty"`
	CostStatus      string            `json:"cost_status,omitempty"`
	InputCostPer1M  float64           `json:"input_cost_per_1m,omitempty"`
	OutputCostPer1M float64           `json:"output_cost_per_1m,omitempty"`
}

func (r PingResult) MarshalJSON() ([]byte, error) {
	type pingResultJSON struct {
		Provider        string            `json:"provider"`
		Model           string            `json:"model"`
		Success         bool              `json:"success"`
		Error           string            `json:"error,omitempty"`
		LatencyMS       int64             `json:"latency_ms"`
		TTFBMS          int64             `json:"ttfb_ms,omitempty"`
		GenerationMS    int64             `json:"generation_duration_ms,omitempty"`
		SpeedTokensPS   float64           `json:"speed_tokens_per_second,omitempty"`
		Response        string            `json:"response,omitempty"`
		TokensIn        int               `json:"tokens_in,omitempty"`
		TokensOut       int               `json:"tokens_out,omitempty"`
		Headers         map[string]string `json:"headers,omitempty"`
		RawResponse     json.RawMessage   `json:"raw_response,omitempty"`
		CostStatus      string            `json:"cost_status,omitempty"`
		InputCostPer1M  float64           `json:"input_cost_per_1m,omitempty"`
		OutputCostPer1M float64           `json:"output_cost_per_1m,omitempty"`
	}
	return json.Marshal(pingResultJSON{
		Provider:        r.Provider,
		Model:           r.Model,
		Success:         r.Success,
		Error:           r.Error,
		LatencyMS:       r.Latency.Milliseconds(),
		TTFBMS:          r.TTFB.Milliseconds(),
		GenerationMS:    r.Generation.Milliseconds(),
		SpeedTokensPS:   r.SpeedTokensPS,
		Response:        r.Response,
		TokensIn:        r.TokensIn,
		TokensOut:       r.TokensOut,
		Headers:         r.Headers,
		RawResponse:     r.RawResponse,
		CostStatus:      r.CostStatus,
		InputCostPer1M:  r.InputCostPer1M,
		OutputCostPer1M: r.OutputCostPer1M,
	})
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

func (cliOpts *invocationOptions) runPing(cmd *commandIO, args []string) error {
	out := cmd.OutOrStdout()
	runCtx, err := cliOpts.preparePingRunWithIO(cmd)
	if err != nil {
		return err
	}

	printPingPlan(out, runCtx)

	results := cliOpts.executePingTargetsContext(cmd.Context(), runCtx.Targets, runCtx.Prompt)
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	if runCtx.RecordRouteMetrics {
		if err := recordPingRoutingMetrics(results, runCtx.Routing); err != nil {
			return err
		}
		fmt.Fprintf(out, "Routing metrics updated: %s\n", runCtx.Routing.MetricsPath)
	}
	if err := recordPingCatalogHealth(cmd.Context(), results); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
	}

	// Sort results by latency ascending (fastest first)
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Latency < results[j].Latency
	})

	printResultsTable(out, results)

	// Summary
	printSummary(out, results)

	// Write output if requested
	if cliOpts.pingOutput != "" {
		if err := writePingOutput(cliOpts.pingOutput, results); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nResults written to %s\n", cliOpts.pingOutput)
	}

	return nil
}

func (cliOpts *invocationOptions) preparePingRun() (pingRunContext, error) {
	return cliOpts.preparePingRunWithIO(&commandIO{ctx: context.Background(), stdout: io.Discard, stderr: io.Discard})
}

func (cliOpts *invocationOptions) preparePingRunWithIO(cmd *commandIO) (pingRunContext, error) {
	if err := cliOpts.validatePingFlags(); err != nil {
		return pingRunContext{}, err
	}

	runCtx := pingRunContext{Prompt: cliOpts.pingPrompt}

	targets, routing, err := cliOpts.loadPingTargetsWithIO(cmd)
	if err != nil {
		return pingRunContext{}, err
	}
	runCtx.Targets = targets
	runCtx.Routing = routing
	runCtx.RecordRouteMetrics = cliOpts.pingRecordMetrics

	return runCtx, nil
}

func (cliOpts *invocationOptions) validatePingFlags() error {
	if cliOpts.pingTimeout <= 0 {
		return fmt.Errorf("invalid --timeout-seconds %d (must be > 0)", cliOpts.pingTimeout)
	}

	return nil
}

func (cliOpts *invocationOptions) loadPingTargets() ([]pingTarget, providers.RoutingConfig, error) {
	return cliOpts.loadPingTargetsWithIO(&commandIO{ctx: context.Background(), stdout: io.Discard, stderr: io.Discard})
}

func (cliOpts *invocationOptions) loadPingTargetsWithIO(cmd *commandIO) ([]pingTarget, providers.RoutingConfig, error) {
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return nil, providers.RoutingConfig{}, fmt.Errorf("load config: %w", err)
	}
	cfg.Routing.ApplyDefaults()

	// Global cap from config applies when the per-call flag is unset (0).
	effectiveMaxOutputCost := cliOpts.pingMaxOutputCost
	effectiveIncludeUnknownCost := cliOpts.pingIncludeUnknownCost
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

	if cliOpts.pingModels != "" || cliOpts.pingFreeOnly || cliOpts.pingMaxOutputCost > 0 {
		selector := cliOpts.pingModels
		if selector == "" && cliOpts.pingFreeOnly {
			selector = "free"
		}
		catPath, err := catalog.CatalogPath()
		if err != nil {
			return nil, providers.RoutingConfig{}, err
		}
		selected, err := resolveTextChatSelection(cmd.ErrOrStderr(), catPath, cfg.Providers, costMap, catalog.SelectorOptions{
			Selector:           selector,
			ProviderFilter:     cliOpts.pingProviders,
			IncludeQuarantine:  cliOpts.pingIncludeQuarantine,
			FreeOnly:           cliOpts.pingFreeOnly,
			IncludeUnknownCost: effectiveIncludeUnknownCost,
			MaxOutputCost:      effectiveMaxOutputCost,
			Blocklist:          providers.NewBlocklist(cfg.Blocklist),
		})
		if err != nil {
			return nil, providers.RoutingConfig{}, err
		}
		targets := make([]pingTarget, 0, len(selected))
		for _, target := range selected {
			targets = append(targets, pingTarget{
				Name:       target.Provider,
				Config:     target.Config,
				CostStatus: target.CostStatus,
				InputCost:  target.InputPer1M,
				OutputCost: target.OutputPer1M,
			})
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

	targets = filterByProvider(targets, cliOpts.pingProviders)
	if cliOpts.pingProviders != "" && len(targets) == 0 {
		return nil, providers.RoutingConfig{}, fmt.Errorf("no targets match --provider %q", cliOpts.pingProviders)
	}

	return targets, cfg.Routing, nil
}

// Scalar helpers retain their signatures with independent default options.
func loadPingTargetsWithIO(cmd *commandIO) ([]pingTarget, providers.RoutingConfig, error) {
	return defaultInvocationOptions().loadPingTargetsWithIO(cmd)
}

func loadPingTargets() ([]pingTarget, providers.RoutingConfig, error) {
	return defaultInvocationOptions().loadPingTargets()
}

func validatePingFlags() error {
	return defaultInvocationOptions().validatePingFlags()
}

func preparePingRunWithIO(cmd *commandIO) (pingRunContext, error) {
	return defaultInvocationOptions().preparePingRunWithIO(cmd)
}

func preparePingRun() (pingRunContext, error) {
	return defaultInvocationOptions().preparePingRun()
}

func runPing(cmd *commandIO, args []string) error {
	return defaultInvocationOptions().runPing(cmd, args)
}
