package providers

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dotcommander/llambo/internal/filetxn"
	"os"
)

func writeGlobalConfig(cfg *GlobalConfig) error {
	if cfg != nil {
		if err := validateProviderNames(cfg.Providers); err != nil {
			return err
		}
	}
	return filetxn.WithLock(context.TODO(), configFile, func(path string) error { return publishGlobalConfig(path, cfg) })
}

func publishGlobalConfig(path string, cfg *GlobalConfig) error {
	if cfg != nil {
		if err := validateProviderNames(cfg.Providers); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return filetxn.WriteAtomic(path, data, 0o600)
}

// UpdateGlobalConfig applies one mutation to freshly decoded persisted config.
// It never materializes environment credentials or runtime defaults. Cooperating
// writers share the canonical retained lock; external editors are not covered.
func UpdateGlobalConfig(ctx context.Context, mutate func(*GlobalConfig) error) error {
	return updateGlobalConfigPath(ctx, configFile, mutate)
}

func updateGlobalConfigPath(ctx context.Context, path string, mutate func(*GlobalConfig) error) error {
	if mutate == nil {
		return errors.New("config mutation is nil")
	}
	return filetxn.WithLock(ctx, path, func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		cfg := &GlobalConfig{}
		if err := json.Unmarshal(data, cfg); err != nil {
			return err
		}
		if err := mutate(cfg); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return publishGlobalConfig(path, cfg)
	})
}
