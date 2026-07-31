package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dotcommander/llambo/providers"
)

var (
	canaryStartProvider     string
	canaryStartTrafficPct   float64
	canaryStartPromoteAfter int
	canaryStartBaseline     string
)

func runCanaryStart(cmd *commandIO, _ []string) error {
	cfg, err := providers.LoadRawGlobalConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	pcfg, ok := cfg.Providers[canaryStartProvider]
	if !ok {
		return fmt.Errorf("provider %q not found in config", canaryStartProvider)
	}
	if !pcfg.Enabled {
		return fmt.Errorf("provider %q is not enabled", canaryStartProvider)
	}
	if canaryStartTrafficPct <= 0 || canaryStartTrafficPct > 1.0 {
		return fmt.Errorf("--traffic must be in (0.0, 1.0]")
	}

	cfg.Routing.Canary = &providers.CanaryConfig{
		Provider:     canaryStartProvider,
		TrafficPct:   canaryStartTrafficPct,
		PromoteAfter: canaryStartPromoteAfter,
		Baseline:     canaryStartBaseline,
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if err := providers.SaveGlobalConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Canary started: %s at %.0f%% traffic\n", canaryStartProvider, canaryStartTrafficPct*100)
	return nil
}

func runCanaryStatus(cmd *commandIO, _ []string) error {
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Routing.Canary == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "No canary configured")
		return nil
	}

	events, err := providers.ReadRouteEvents(cfg.Routing.EventsPath)
	if err != nil {
		return fmt.Errorf("read route events: %w", err)
	}

	status := providers.EvaluateCanary(cfg.Routing.Canary, events, cfg.Providers)
	if status == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "No canary configured")
		return nil
	}

	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func runCanaryPromote(cmd *commandIO, _ []string) error {
	cfg, err := providers.LoadRawGlobalConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Routing.Canary == nil {
		return fmt.Errorf("no canary configured")
	}

	canaryName := cfg.Routing.Canary.Provider
	baselineName := cfg.Routing.Canary.Baseline

	targetPriority := 1
	if baselineName != "" {
		if bcfg, ok := cfg.Providers[baselineName]; ok {
			targetPriority = bcfg.Priority
		}
	} else {
		for _, pcfg := range cfg.Providers {
			if pcfg.Enabled && pcfg.Priority > 0 && pcfg.Priority < targetPriority {
				targetPriority = pcfg.Priority
			}
		}
	}

	if pcfg, ok := cfg.Providers[canaryName]; ok {
		pcfg.Priority = targetPriority
		cfg.Providers[canaryName] = pcfg
	}

	cfg.Routing.Canary = nil

	if err := providers.SaveGlobalConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Promoted %s to priority %d, canary config removed\n", canaryName, targetPriority)
	return nil
}

func runCanaryStop(cmd *commandIO, _ []string) error {
	cfg, err := providers.LoadRawGlobalConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Routing.Canary == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "No canary configured")
		return nil
	}

	provider := cfg.Routing.Canary.Provider
	cfg.Routing.Canary = nil

	if err := providers.SaveGlobalConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Canary routing stopped for %s\n", provider)
	return nil
}
