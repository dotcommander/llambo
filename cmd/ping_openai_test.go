package cmd

import (
	"testing"

	"github.com/dotcommander/llambo/providers"
)

func TestPingOpenAIProviderOptionsAreRequestIsolated(t *testing.T) {
	t.Parallel()
	cfg := providers.Config{
		Model: "exact-model",
		ExtraBody: map[string]any{
			"provider_only": map[string]any{"provider": true},
			"replaced":      map[string]any{"provider": true},
		},
		ExtraBodyByModel: map[string]map[string]any{
			"exact-model": {
				"replaced": map[string]any{"model": true},
			},
		},
	}
	client := pingOpenAIClient{
		model:            cfg.Model,
		extraBody:        cfg.ExtraBody,
		extraBodyByModel: cfg.ExtraBodyByModel,
	}

	first := client.providerOptions()
	first["provider_only"].(map[string]any)["provider"] = false
	first["replaced"].(map[string]any)["model"] = false
	if cfg.ExtraBody["provider_only"].(map[string]any)["provider"] != true || cfg.ExtraBodyByModel[cfg.Model]["replaced"].(map[string]any)["model"] != true {
		t.Fatalf("request mutation changed config: %#v", cfg)
	}
	cfg.ExtraBody["provider_only"].(map[string]any)["provider"] = "updated"
	cfg.ExtraBodyByModel[cfg.Model]["replaced"].(map[string]any)["model"] = "updated"
	second := client.providerOptions()
	if second["provider_only"].(map[string]any)["provider"] != "updated" || second["replaced"].(map[string]any)["model"] != "updated" {
		t.Fatalf("request options did not use retained sources: %#v", second)
	}
	if first["provider_only"].(map[string]any)["provider"] != false || first["replaced"].(map[string]any)["model"] != false {
		t.Fatalf("source mutation changed prior request options: %#v", first)
	}
}
