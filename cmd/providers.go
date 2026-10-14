package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/providers"
)

// Repeated string literals consolidated by goconst.
const (
	never1 = "never"
)

func runProviders(cmd *commandIO) error {
	out := cmd.OutOrStdout()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}

	catPath, err := catalog.Path()
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
		enabledStr := no1
		if pcfg.Enabled {
			enabledStr = yes1
		}

		summary := providerCatalogSummary(cat, name)

		fmt.Fprintf(out, "%-14s %-8s %-42s %-7d %-7d %-7d %s\n",
			name, enabledStr, truncatedBaseURL(pcfg.BaseURL), summary.models, summary.pinned, summary.avoided, summary.lastRefresh)
	}
	return nil
}

type providerCatalogCounts struct {
	models      int
	pinned      int
	avoided     int
	lastRefresh string
}

// providerCatalogSummary summarizes tracked models, pin/avoid counts, and the
// last refresh age for one configured provider.
func providerCatalogSummary(cat *catalog.Catalog, name string) providerCatalogCounts {
	summary := providerCatalogCounts{lastRefresh: never1}
	pc := cat.Providers[name]
	if pc == nil {
		return summary
	}
	for _, m := range pc.Models {
		if m.Pinned {
			summary.pinned++
		}
		if m.Avoid {
			summary.avoided++
		}
	}
	if !pc.LastRefresh.IsZero() {
		summary.lastRefresh = humanDuration(time.Since(pc.LastRefresh)) + " ago"
	}
	summary.models = len(pc.Models)
	return summary
}

// truncatedBaseURL shortens long base URLs for the fixed-width table.
func truncatedBaseURL(base string) string {
	if len(base) <= 40 {
		return base
	}
	return base[:37] + "..."
}

func runProvidersRefresh(cmd *commandIO, args []string, opts catalog.RefreshOptions) error {
	out := cmd.OutOrStdout()
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}

	catPath, err := catalog.Path()
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
			refreshedAgo := never1
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
