package cmd

import (
	"fmt"
	"strings"

	"github.com/dotcommander/llambo/providers"
)

func runConfigInit(cmd *commandIO) error {
	out := cmd.OutOrStdout()
	if err := providers.InitDefaultConfig(); err != nil {
		return err
	}
	preferencesPath, err := routePreferencesPath()
	if err != nil {
		return err
	}
	if err := writeDefaultRoutePreferences(preferencesPath); err != nil {
		return err
	}
	fmt.Fprintln(out, "Created ~/.config/llambo/config.json")
	fmt.Fprintln(out, "Created ~/.config/llambo/route-preferences.yaml")
	fmt.Fprintln(out, "\nEdit config.json to configure providers:")
	fmt.Fprintln(out, "  - Set enabled: true for providers you want to use")
	fmt.Fprintln(out, "  - Add api_key or set env var (e.g., OPENROUTER_API_KEY)")
	fmt.Fprintln(out, "  - Set model to your preferred model")
	fmt.Fprintln(out, "Edit route-preferences.yaml to configure task routing preferences")
	return nil
}

func runConfigShow(cmd *commandIO) error {
	out := cmd.OutOrStdout()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Default provider: %s\n\n", cfg.DefaultProvider)
	fmt.Fprintln(out, "Providers:")

	for _, name := range sortedProviderNames(cfg.Providers) {
		pcfg := cfg.Providers[name]
		hasKey := "✗"
		apiKey := providers.GetAPIKey(name, pcfg)
		if apiKey != "" || !pcfg.GetRequiresKey() {
			hasKey = "✓"
		}
		fmt.Fprintf(out, "  %s: %s (model: %s) [key: %s]\n", name, pcfg.BaseURL, pcfg.Model, hasKey)
	}

	cfg.Routing.ApplyDefaults()
	fmt.Fprintln(out, "\nRouting:")
	fmt.Fprintf(out, "  mode: %s\n", cfg.Routing.Mode)
	if cfg.Routing.MaxCostUSD > 0 {
		fmt.Fprintf(out, "  max_cost_usd: %.6f\n", cfg.Routing.MaxCostUSD)
	}
	if cfg.Routing.MaxLatencyMs > 0 {
		fmt.Fprintf(out, "  max_latency_ms: %d\n", cfg.Routing.MaxLatencyMs)
	}
	if len(cfg.Routing.AllowedProviders) > 0 {
		fmt.Fprintf(out, "  allowed_providers: %v\n", cfg.Routing.AllowedProviders)
	}
	if len(cfg.Routing.DeniedProviders) > 0 {
		fmt.Fprintf(out, "  denied_providers: %v\n", cfg.Routing.DeniedProviders)
	}
	if len(cfg.Routing.DailyMaxRequests) > 0 {
		fmt.Fprintf(out, "  daily_max_requests: %v\n", cfg.Routing.DailyMaxRequests)
	}
	if len(cfg.Routing.DailyMaxTokens) > 0 {
		fmt.Fprintf(out, "  daily_max_tokens: %v\n", cfg.Routing.DailyMaxTokens)
	}
	if len(cfg.Routing.DailyMaxCostUSD) > 0 {
		fmt.Fprintf(out, "  daily_max_cost_usd: %v\n", cfg.Routing.DailyMaxCostUSD)
	}
	fmt.Fprintf(out, "  metrics_path: %s\n", cfg.Routing.MetricsPath)
	fmt.Fprintf(out, "  events_path: %s\n", cfg.Routing.EventsPath)

	cfg.Gateway.ApplyDefaults()
	fmt.Fprintln(out, "\nGateway:")
	fmt.Fprintf(out, "  max_active_jobs: %d\n", cfg.Gateway.MaxActiveJobs)
	fmt.Fprintf(out, "  max_requests_per_job: %d\n", cfg.Gateway.MaxRequestsPerJob)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Limits:")
	if cfg.MaxOutputCost > 0 {
		fmt.Fprintf(out, "  max_output_cost: $%.2f per 1M output tokens\n", cfg.MaxOutputCost)
	} else {
		fmt.Fprintln(out, "  max_output_cost: (disabled)")
	}
	if len(cfg.Blocklist) > 0 {
		fmt.Fprintf(out, "  blocklist (%d): %s\n", len(cfg.Blocklist), strings.Join(cfg.Blocklist, ", "))
	} else {
		fmt.Fprintln(out, "  blocklist: (none)")
	}
	return nil
}
