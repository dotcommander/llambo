package cmd

import (
	"fmt"

	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/modelsdev"
	"github.com/dotcommander/llambo/providers"
)

func (cliOpts *invocationOptions) runModelsSyncPricing(cmd *commandIO, _ []string) error {
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
		Filter:        cliOpts.modelsSyncPricingProviders,
		OutPath:       costs.ModelsDevPath(),
		DryRun:        cliOpts.modelsSyncPricingDryRun,
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "models.dev pricing: %d models across %d providers (added %d, changed %d, removed %d)\n",
		res.Models, res.Providers, res.Added, res.Changed, res.Removed)
	if cliOpts.modelsSyncPricingDryRun {
		fmt.Fprintln(out, "(dry-run — pricing file not written)")
	} else {
		fmt.Fprintf(out, "wrote %s\n", costs.ModelsDevPath())
	}
	return nil
}

// Scalar helpers retain their signatures with independent default options.
func runModelsSyncPricing(cmd *commandIO, ignored1 []string) error {
	return defaultInvocationOptions().runModelsSyncPricing(cmd, ignored1)
}
