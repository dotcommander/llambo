package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

func verifyWritingOMLXModels(ctx context.Context, manifest evals.WritingRunManifest, configs map[string]providers.Config) error {
	models := append([]evals.WritingModelSpec(nil), manifest.Identity.Models...)
	models = append(models, manifest.Identity.Judge)
	for _, model := range models {
		if model.Provider != "omlx" {
			continue
		}
		cfg, ok := configs[model.ID()]
		if !ok || strings.TrimSpace(cfg.BaseURL) == "" {
			return fmt.Errorf("verify live OMLX model %s: runtime base URL is unavailable", model.ID())
		}
		_, err := evals.VerifyOMLXExactModel(ctx, cfg.BaseURL, "", model.Model, nil)
		if err != nil && strings.Contains(err.Error(), "request OMLX endpoint") {
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			_, err = evals.VerifyOMLXExactModel(ctx, cfg.BaseURL, "", model.Model, nil)
		}
		if err != nil {
			return fmt.Errorf("verify live OMLX model %s: %w", model.ID(), err)
		}
	}
	return nil
}
