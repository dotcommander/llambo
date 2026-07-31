package providers

import (
	"fmt"
	"slices"
	"strings"
)

// ProviderEntry pairs a provider name with its config.
type ProviderEntry struct {
	Name   string
	Config Config
}

func providerNames(providers map[string]Config) string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// FilterEnabledProviders returns enabled providers in priority order.
func FilterEnabledProviders(configs map[string]Config) []ProviderEntry {
	var entries []ProviderEntry
	for _, name := range GetProviderOrderFromConfigs(configs) {
		cfg, ok := configs[name]
		if !ok || !cfg.Enabled {
			continue
		}
		entries = append(entries, ProviderEntry{Name: name, Config: cfg})
	}
	return entries
}

// GetProviderConfig returns config for a specific provider.
func GetProviderConfig(name string) (Config, error) {
	globalCfg, err := LoadGlobalConfig()
	if err != nil {
		return Config{}, err
	}
	if name == "" {
		name = globalCfg.DefaultProvider
	}
	cfg, ok := globalCfg.Providers[name]
	if !ok {
		return Config{}, fmt.Errorf("provider %q not found in config", name)
	}
	if cfg.APIKey == "" {
		cfg.APIKey = GetAPIKey(name, cfg)
	}
	return cfg, nil
}
