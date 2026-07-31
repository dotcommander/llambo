package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayConfigResolveAuthTokenDirect(t *testing.T) {
	t.Parallel()
	const token = "local-config-token"
	got, err := (GatewayConfig{AuthToken: token}).ResolveAuthToken()
	if err != nil {
		t.Fatalf("ResolveAuthToken: %v", err)
	}
	if got != token {
		t.Fatalf("token = %q, want configured token", got)
	}
}

func TestLoadGlobalConfig_RequiresKeyDefaultsTrueWhenOmitted(t *testing.T) {
	setTestConfigPath(t)
	t.Setenv("LLAMBO_TEST_MISSING_API_KEY", "")

	writeTestConfig(t, `{
		"default_provider": "needskey",
		"providers": {
			"needskey": {
				"provider_type": "openai",
				"base_url": "https://api.openai.com",
				"model": "gpt-test",
				"enabled": true,
				"env_var": "LLAMBO_TEST_MISSING_API_KEY"
			}
		}
	}`)

	_, err := LoadGlobalConfig()
	if err == nil {
		t.Fatal("expected missing API key error")
	}
	if !strings.Contains(err.Error(), "provider \"needskey\" missing API key") {
		t.Fatalf("expected missing API key error, got %v", err)
	}
}

func TestLoadGlobalConfig_RequiresKeyExplicitFalseAllowsNoKey(t *testing.T) {
	setTestConfigPath(t)

	writeTestConfig(t, `{
		"default_provider": "local",
		"providers": {
			"local": {
				"provider_type": "openai",
				"base_url": "http://localhost:1234",
				"model": "local-model",
				"enabled": true,
				"requires_key": false
			}
		}
	}`)

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if cfg.Providers["local"].GetRequiresKey() {
		t.Fatal("expected explicit requires_key=false to be preserved")
	}
}

func TestLoadGlobalConfig_RequiresKeyExplicitTrueUsesEnvFallback(t *testing.T) {
	setTestConfigPath(t)
	t.Setenv("OPENAI_API_KEY", "test-key")

	writeTestConfig(t, `{
		"default_provider": "openai",
		"providers": {
			"openai": {
				"provider_type": "openai",
				"base_url": "https://api.openai.com",
				"model": "gpt-test",
				"enabled": true,
				"requires_key": true
			}
		}
	}`)

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if got := cfg.Providers["openai"].APIKey; got != "test-key" {
		t.Fatalf("expected env API key, got %q", got)
	}
}

func TestLoadRawGlobalConfigDoesNotPersistEnvFallback(t *testing.T) {
	setTestConfigPath(t)
	t.Setenv("OPENAI_API_KEY", "test-key")

	writeTestConfig(t, `{
		"default_provider": "openai",
		"providers": {
			"openai": {
				"provider_type": "openai",
				"base_url": "https://api.openai.com",
				"model": "gpt-test",
				"enabled": true,
				"requires_key": true
			}
		}
	}`)

	cfg, err := LoadRawGlobalConfig()
	if err != nil {
		t.Fatalf("LoadRawGlobalConfig: %v", err)
	}
	if got := cfg.Providers["openai"].APIKey; got != "" {
		t.Fatalf("expected raw config to avoid env API key, got %q", got)
	}
}

func TestInitDefaultConfigUsesCurrentZAIModel(t *testing.T) {
	setTestConfigPath(t)

	if err := InitDefaultConfig(); err != nil {
		t.Fatalf("InitDefaultConfig: %v", err)
	}

	cfg, err := LoadRawGlobalConfig()
	if err != nil {
		t.Fatalf("LoadRawGlobalConfig: %v", err)
	}
	if got := cfg.Providers["zai"].Model; got != "GLM-5.2" {
		t.Fatalf("zai model = %q, want %q", got, "GLM-5.2")
	}
}

func TestResolveModelsDevKey(t *testing.T) {
	tests := []struct {
		name         string
		providerName string
		cfg          Config
		want         string
	}{
		{
			name:         "explicit override wins",
			providerName: "gemini",
			cfg:          Config{ModelsDevKey: "custom"},
			want:         "custom",
		},
		{
			name:         "gemini provider type maps to google",
			providerName: "gemini",
			cfg:          Config{ProviderType: "gemini"},
			want:         "google",
		},
		{
			name:         "gemini base URL maps to google",
			providerName: "gemini",
			cfg:          Config{BaseURL: "https://generativelanguage.googleapis.com/v1beta"},
			want:         "google",
		},
		{
			name:         "default uses provider name",
			providerName: "openai",
			cfg:          Config{ProviderType: "openai", BaseURL: "https://api.openai.com"},
			want:         "openai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveModelsDevKey(tt.providerName, tt.cfg); got != tt.want {
				t.Fatalf("ResolveModelsDevKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetConfigFileOverridesConfigPath(t *testing.T) {
	oldConfigDir := configDir
	oldConfigFile := configFile
	t.Cleanup(func() {
		configDir = oldConfigDir
		configFile = oldConfigFile
	})

	path := filepath.Join(t.TempDir(), "custom.json")
	SetConfigFile(path)
	if ConfigFile() != path {
		t.Fatalf("ConfigFile() = %q, want %q", ConfigFile(), path)
	}
	if configDir != filepath.Dir(path) {
		t.Fatalf("configDir = %q, want %q", configDir, filepath.Dir(path))
	}
}

func setTestConfigPath(t *testing.T) {
	t.Helper()

	oldConfigDir := configDir
	oldConfigFile := configFile
	dir := t.TempDir()
	configDir = dir
	configFile = filepath.Join(dir, "config.json")
	t.Cleanup(func() {
		configDir = oldConfigDir
		configFile = oldConfigFile
	})
}

func writeTestConfig(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(configFile, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
