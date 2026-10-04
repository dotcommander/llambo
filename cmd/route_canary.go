package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dotcommander/llambo/providers"
)

func (cliOpts *invocationOptions) runCanaryStart(cmd *commandIO, _ []string) error {
	if err := providers.UpdateGlobalConfig(cmd.Context(), func(cfg *providers.GlobalConfig) error {
		pcfg, ok := cfg.Providers[cliOpts.canaryStartProvider]
		if !ok {
			return fmt.Errorf("provider %q not found in config", cliOpts.canaryStartProvider)
		}
		if !pcfg.Enabled {
			return fmt.Errorf("provider %q is not enabled", cliOpts.canaryStartProvider)
		}
		if cliOpts.canaryStartTrafficPct <= 0 || cliOpts.canaryStartTrafficPct > 1.0 {
			return fmt.Errorf("--traffic must be in (0.0, 1.0]")
		}
		cfg.Routing.Canary = &providers.CanaryConfig{Provider: cliOpts.canaryStartProvider, TrafficPct: cliOpts.canaryStartTrafficPct, PromoteAfter: cliOpts.canaryStartPromoteAfter, Baseline: cliOpts.canaryStartBaseline, StartedAt: time.Now().UTC().Format(time.RFC3339)}
		return nil
	}); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Canary started: %s at %.0f%% traffic\n", cliOpts.canaryStartProvider, cliOpts.canaryStartTrafficPct*100)
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
	var canaryName string
	var targetPriority int
	if err := providers.UpdateGlobalConfig(cmd.Context(), func(cfg *providers.GlobalConfig) error {
		if cfg.Routing.Canary == nil {
			return fmt.Errorf("no canary configured")
		}
		canaryName, targetPriority = providers.PromoteCanaryConfig(cfg, *cfg.Routing.Canary)
		return nil
	}); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Promoted %s to priority %d, canary config removed\n", canaryName, targetPriority)
	return nil
}

func runCanaryStop(cmd *commandIO, _ []string) error {
	var provider string
	if err := providers.UpdateGlobalConfig(cmd.Context(), func(cfg *providers.GlobalConfig) error {
		if cfg.Routing.Canary != nil {
			provider = cfg.Routing.Canary.Provider
			cfg.Routing.Canary = nil
		}
		return nil
	}); err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	if provider == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "No canary configured")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Canary routing stopped for %s\n", provider)
	return nil
}

// Scalar helpers retain their signatures with independent default options.
func runCanaryStart(cmd *commandIO, ignored1 []string) error {
	return defaultInvocationOptions().runCanaryStart(cmd, ignored1)
}
