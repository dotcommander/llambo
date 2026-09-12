package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/providers"
)

func runProviders(cmd *commandIO, args []string) error {
	out := cmd.OutOrStdout()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}

	catPath, err := catalog.CatalogPath()
	if err != nil {
		return err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "%-14s %-8s %-42s %-7s %-7s %-7s %s\n",
		"PROVIDER", "ENABLED", "BASE_URL", "MODELS", "PINNED", "AVOID", "LAST_REFRESH")
	fmt.Fprintln(out, strings.Repeat("-", 100))

	for _, name := range sortedProviderNames(cfg.Providers) {
		pcfg := cfg.Providers[name]
		enabledStr := "no"
		if pcfg.Enabled {
			enabledStr = "yes"
		}

		modelCount, pinned, avoided := 0, 0, 0
		lastRefreshStr := "never"

		if pc := cat.Providers[name]; pc != nil {
			modelCount = len(pc.Models)
			for _, m := range pc.Models {
				if m.Pinned {
					pinned++
				}
				if m.Avoid {
					avoided++
				}
			}
			if !pc.LastRefresh.IsZero() {
				lastRefreshStr = humanDuration(time.Since(pc.LastRefresh)) + " ago"
			}
		}

		base := pcfg.BaseURL
		if len(base) > 40 {
			base = base[:37] + "..."
		}

		fmt.Fprintf(out, "%-14s %-8s %-42s %-7d %-7d %-7d %s\n",
			name, enabledStr, base, modelCount, pinned, avoided, lastRefreshStr)
	}
	return nil
}

func runProvidersRefresh(cmd *commandIO, args []string, opts catalog.RefreshOptions) error {
	out := cmd.OutOrStdout()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}

	catPath, err := catalog.CatalogPath()
	if err != nil {
		return err
	}

	results, err := catalog.RefreshWithOptions(cmd.Context(), args, cfg.Providers, catPath, opts)
	if err != nil {
		return fmt.Errorf("save catalog: %w", err)
	}

	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(out, "%-14s ✗ failed: %s\n", r.Provider, r.Err)
			continue
		}
		if r.Cached {
			refreshedAgo := "never"
			if !r.LastRefresh.IsZero() {
				refreshedAgo = humanDuration(time.Since(r.LastRefresh)) + " ago"
			}
			fmt.Fprintf(out, "%-14s ✓ %d models (cached, refreshed %s, TTL %s) from %s\n",
				r.Provider, r.Total, refreshedAgo, humanDuration(opts.TTL), r.Endpoint)
			continue
		}
		summary := fmt.Sprintf("+%d new, %d updated, -%d stale, ~%d unchanged",
			len(r.NewIDs), r.UpdatedN, len(r.StaleIDs), r.UnchangedN)
		fmt.Fprintf(out, "%-14s ✓ %d models (%s) from %s\n",
			r.Provider, r.Total, summary, r.Endpoint)
	}
	return nil
}

// humanDuration renders a duration as a concise human string (e.g. "2h", "3d").
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
