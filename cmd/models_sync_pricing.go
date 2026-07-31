package cmd

import (
	"fmt"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/modelsdev"
	"github.com/dotcommander/llambo/providers"
)

var modelsSyncPricingDryRun bool
var modelsSyncPricingProviders []string

func runModelsSyncPricing(cmd *commandIO, _ []string) error {
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return err
	}

	providerToKey := make(map[string]string, len(cfg.Providers))
	for name, pc := range cfg.Providers {
		providerToKey[name] = providers.ResolveModelsDevKey(name, pc)
	}

	res, _, err := modelsdev.RunSync(cmd.Context(), modelsdev.SyncOptions{
		ProviderToKey: providerToKey,
		Filter:        modelsSyncPricingProviders,
		OutPath:       costs.ModelsDevPath(),
		DryRun:        modelsSyncPricingDryRun,
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "models.dev pricing: %d models across %d providers (added %d, changed %d, removed %d)\n",
		res.Models, res.Providers, res.Added, res.Changed, res.Removed)
	if modelsSyncPricingDryRun {
		fmt.Fprintln(out, "(dry-run — pricing file not written)")
	} else {
		fmt.Fprintf(out, "wrote %s\n", costs.ModelsDevPath())
	}
	return nil
}
