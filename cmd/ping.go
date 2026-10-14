package cmd

import (
	"encoding/json"
	"errors"
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

// MarshalJSON renders the ping result in its stable report shape.
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

func (cliOpts *invocationOptions) runPing(cmd *commandIO) error {
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

// pingCostCaps carries the resolved output-cost gating shared by ping target
// selection paths.
type pingCostCaps struct {
	maxOutputCost      float64
	includeUnknownCost bool
}

// resolvePingCostCaps applies the global config cap when the per-call flag is
// unset (0); a configured cap opts unknown-priced models in.
func resolvePingCostCaps(cliOpts *invocationOptions, cfg *providers.GlobalConfig) pingCostCaps {
	caps := pingCostCaps{maxOutputCost: cliOpts.pingMaxOutputCost, includeUnknownCost: cliOpts.pingIncludeUnknownCost}
	if caps.maxOutputCost != 0 {
		return caps
	}
	caps.maxOutputCost = cfg.MaxOutputCost
	if cfg.MaxOutputCost > 0 {
		caps.includeUnknownCost = true
	}
	return caps
}

func (cliOpts *invocationOptions) loadPingTargetsWithIO(cmd *commandIO) ([]pingTarget, providers.RoutingConfig, error) {
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return nil, providers.RoutingConfig{}, fmt.Errorf("load config: %w", err)
	}
	cfg.Routing.ApplyDefaults()

	costMap, err := costs.LoadAll()
	if err != nil {
		return nil, providers.RoutingConfig{}, fmt.Errorf("load model costs: %w", err)
	}
	caps := resolvePingCostCaps(cliOpts, cfg)
	errOut := cmd.ErrOrStderr()

	if cliOpts.pingModels != "" || cliOpts.pingFreeOnly || cliOpts.pingMaxOutputCost > 0 {
		targets, selectionErr := cliOpts.selectPingTargetsFromCatalog(errOut, cfg, costMap, caps)
		if selectionErr != nil {
			return nil, providers.RoutingConfig{}, selectionErr
		}
		return targets, cfg.Routing, nil
	}

	targets, err := cliOpts.configuredPingTargets(errOut, cfg, costMap, caps)
	if err != nil {
		return nil, providers.RoutingConfig{}, err
	}
	return targets, cfg.Routing, nil
}

// selectPingTargetsFromCatalog resolves targets through the catalog text-chat
// selector when a model selector, free-only, or cost-cap filter is active.
func (cliOpts *invocationOptions) selectPingTargetsFromCatalog(errOut io.Writer, cfg *providers.GlobalConfig, costMap map[string]costs.ModelCost, caps pingCostCaps) ([]pingTarget, error) {
	selector := cliOpts.pingModels
	if selector == "" && cliOpts.pingFreeOnly {
		selector = free1
	}
	catPath, err := catalog.Path()
	if err != nil {
		return nil, err
	}
	selected, err := resolveTextChatSelection(errOut, catPath, cfg.Providers, costMap, catalog.SelectorOptions{
		Selector:           selector,
		ProviderFilter:     cliOpts.pingProviders,
		IncludeQuarantine:  cliOpts.pingIncludeQuarantine,
		FreeOnly:           cliOpts.pingFreeOnly,
		IncludeUnknownCost: caps.includeUnknownCost,
		MaxOutputCost:      caps.maxOutputCost,
		Blocklist:          providers.NewBlocklist(cfg.Blocklist),
	})
	if err != nil {
		return nil, err
	}
	return pingTargetsFromSelection(selected), nil
}

// configuredPingTargets narrows all configured providers to ping targets using
// cost, blocklist, and text-chat policy.
func (cliOpts *invocationOptions) configuredPingTargets(errOut io.Writer, cfg *providers.GlobalConfig, costMap map[string]costs.ModelCost, caps pingCostCaps) ([]pingTarget, error) {
	cat, err := loadCatalogForPingFilter()
	if err != nil {
		return nil, err
	}
	targets := buildPingTargetsWithWriter(errOut, cfg.Providers, pingTargetFilter{
		costMap:        costMap,
		blocklist:      providers.NewBlocklist(cfg.Blocklist),
		maxOutputCost:  caps.maxOutputCost,
		includeUnknown: caps.includeUnknownCost,
		catalog:        cat,
	})
	if len(targets) == 0 {
		return nil, errors.New("no enabled providers found")
	}

	targets = filterByProvider(targets, cliOpts.pingProviders)
	if cliOpts.pingProviders != "" && len(targets) == 0 {
		return nil, fmt.Errorf("no targets match --provider %q", cliOpts.pingProviders)
	}
	return targets, nil
}
