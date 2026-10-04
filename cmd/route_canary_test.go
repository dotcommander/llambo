package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/llambo/providers"
)

const canaryEnvOnlyKey = "env-only-canary-secret"

func TestCanaryMutationsPersistRawConfig(t *testing.T) {
	legacyTestOptions := invocationOptions{modelsGrouped: true, timeoutSeconds: 60, promptFuseModels: defaultPromptFuseModels}
	t.Setenv("LLAMBO_CANARY_TEST_API_KEY", canaryEnvOnlyKey)

	t.Run("start", func(t *testing.T) {
		configPath := writeCanaryConfig(t, `{
  "default_provider": "baseline",
  "providers": {
    "baseline": {"base_url":"https://baseline.example","model":"baseline","enabled":true,"priority":7,"requires_key":false,"api_key":"configured-baseline-key"},
    "canary": {"base_url":"https://canary.example","model":"canary","enabled":true,"priority":19,"env_var":"LLAMBO_CANARY_TEST_API_KEY"}
  }
}`)
		oldProvider, oldTraffic, oldPromoteAfter, oldBaseline := legacyTestOptions.canaryStartProvider, legacyTestOptions.canaryStartTrafficPct, legacyTestOptions.canaryStartPromoteAfter, legacyTestOptions.canaryStartBaseline
		t.Cleanup(func() {
			legacyTestOptions.canaryStartProvider, legacyTestOptions.canaryStartTrafficPct, legacyTestOptions.canaryStartPromoteAfter, legacyTestOptions.canaryStartBaseline = oldProvider, oldTraffic, oldPromoteAfter, oldBaseline
		})
		legacyTestOptions.canaryStartProvider, legacyTestOptions.canaryStartTrafficPct, legacyTestOptions.canaryStartPromoteAfter, legacyTestOptions.canaryStartBaseline = "canary", 0.25, 12, "baseline"

		if err := legacyTestOptions.runCanaryStart(&commandIO{ctx: context.Background(), stdout: new(bytes.Buffer)}, nil); err != nil {
			t.Fatalf("runCanaryStart: %v", err)
		}

		cfg := assertRawCanaryConfig(t, configPath)
		if cfg.Routing.Canary == nil || cfg.Routing.Canary.Provider != "canary" || cfg.Routing.Canary.TrafficPct != 0.25 || cfg.Routing.Canary.PromoteAfter != 12 || cfg.Routing.Canary.Baseline != "baseline" {
			t.Fatalf("persisted canary = %#v, want configured canary route", cfg.Routing.Canary)
		}
		assertCanaryKeysUnchanged(t, cfg)
	})

	t.Run("promote", func(t *testing.T) {
		configPath := writeCanaryConfig(t, `{
  "default_provider": "baseline",
  "providers": {
    "baseline": {"base_url":"https://baseline.example","model":"baseline","enabled":true,"priority":7,"requires_key":false,"api_key":"configured-baseline-key"},
    "canary": {"base_url":"https://canary.example","model":"canary","enabled":true,"priority":19,"env_var":"LLAMBO_CANARY_TEST_API_KEY"}
  },
  "routing": {"canary":{"provider":"canary","traffic_pct":0.25,"baseline":"baseline"}}
}`)

		if err := runCanaryPromote(&commandIO{ctx: context.Background(), stdout: new(bytes.Buffer)}, nil); err != nil {
			t.Fatalf("runCanaryPromote: %v", err)
		}

		cfg := assertRawCanaryConfig(t, configPath)
		if cfg.Routing.Canary != nil {
			t.Fatalf("persisted canary = %#v, want removed", cfg.Routing.Canary)
		}
		if got := cfg.Providers["canary"].Priority; got != 7 {
			t.Fatalf("canary priority = %d, want baseline priority 7", got)
		}
		assertCanaryKeysUnchanged(t, cfg)
	})

	t.Run("stop", func(t *testing.T) {
		configPath := writeCanaryConfig(t, `{
  "default_provider": "baseline",
  "providers": {
    "baseline": {"base_url":"https://baseline.example","model":"baseline","enabled":true,"priority":7,"requires_key":false,"api_key":"configured-baseline-key"},
    "canary": {"base_url":"https://canary.example","model":"canary","enabled":true,"priority":19,"env_var":"LLAMBO_CANARY_TEST_API_KEY"}
  },
  "routing": {"canary":{"provider":"canary","traffic_pct":0.25,"baseline":"baseline"}}
}`)

		if err := runCanaryStop(&commandIO{ctx: context.Background(), stdout: new(bytes.Buffer)}, nil); err != nil {
			t.Fatalf("runCanaryStop: %v", err)
		}

		cfg := assertRawCanaryConfig(t, configPath)
		if cfg.Routing.Canary != nil {
			t.Fatalf("persisted canary = %#v, want removed", cfg.Routing.Canary)
		}
		if got := cfg.Providers["canary"].Priority; got != 19 {
			t.Fatalf("canary priority = %d, want unchanged priority 19", got)
		}
		assertCanaryKeysUnchanged(t, cfg)
	})
}

func writeCanaryConfig(t *testing.T, config string) string {
	t.Helper()

	oldConfigPath := providers.ConfigFile()
	configPath := filepath.Join(t.TempDir(), "config.json")
	providers.SetConfigFile(configPath)
	t.Cleanup(func() { providers.SetConfigFile(oldConfigPath) })
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func assertRawCanaryConfig(t *testing.T, configPath string) providers.GlobalConfig {
	t.Helper()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	if bytes.Contains(data, []byte(canaryEnvOnlyKey)) {
		t.Fatalf("persisted config contains environment-only API key")
	}
	var cfg providers.GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode persisted config: %v", err)
	}
	return cfg
}

func assertCanaryKeysUnchanged(t *testing.T, cfg providers.GlobalConfig) {
	t.Helper()

	if got := cfg.Providers["canary"].APIKey; got != "" {
		t.Fatalf("canary API key = %q, want environment-only key omitted", got)
	}
	if got := cfg.Providers["baseline"].APIKey; got != "configured-baseline-key" {
		t.Fatalf("baseline API key = %q, want explicit configured key preserved", got)
	}
}
