package providers

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

func TestEffectiveConfigHydratesPrimaryArrayKeyWithoutChangingRawConfig(t *testing.T) {
	setTestConfigPath(t)
	t.Setenv("LLAMBO_TEST_MIXED_PRIMARY_KEY", "environment-key")

	writeTestConfig(t, `{
		"default_provider": "mixed",
		"providers": {
			"mixed": {
				"api_key": "configured-single-key",
				"api_keys": ["array-primary-key", "array-secondary-key"],
				"env_var": "LLAMBO_TEST_MIXED_PRIMARY_KEY",
				"enabled": false
			}
		}
	}`)

	raw, err := LoadRawGlobalConfig()
	if err != nil {
		t.Fatalf("LoadRawGlobalConfig: %v", err)
	}
	if got := raw.Providers["mixed"].APIKey; got != "configured-single-key" {
		t.Fatalf("raw APIKey = %q, want configured single key", got)
	}

	effective, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if got := effective.Providers["mixed"].APIKey; got != "configured-single-key" {
		t.Fatalf("effective APIKey = %q, want configured single key", got)
	}
	if got := GetAPIKey("mixed", effective.Providers["mixed"]); got != "array-primary-key" {
		t.Fatalf("GetAPIKey() = %q, want primary array key", got)
	}
	if got, want := GetAPIKeys("mixed", effective.Providers["mixed"]), []string{"array-primary-key", "array-secondary-key", "configured-single-key", "environment-key"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("GetAPIKeys() = %q, want %q", got, want)
	}

	selected, err := GetProviderConfig("mixed")
	if err != nil {
		t.Fatalf("GetProviderConfig: %v", err)
	}
	if got := selected.APIKey; got != "configured-single-key" {
		t.Fatalf("selected APIKey = %q, want configured single key", got)
	}

	raw, err = LoadRawGlobalConfig()
	if err != nil {
		t.Fatalf("LoadRawGlobalConfig after hydration: %v", err)
	}
	if got := raw.Providers["mixed"].APIKey; got != "configured-single-key" {
		t.Fatalf("raw APIKey after hydration = %q, want configured single key", got)
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

func TestSaveGlobalConfigUsesCanonicalPrivateWriter(t *testing.T) {
	setTestConfigPath(t)
	want := &GlobalConfig{
		DefaultProvider: "local",
		Providers: map[string]Config{
			"local": {ProviderType: "openai", BaseURL: "http://127.0.0.1:8000", Model: "test-model", Enabled: true, RequiresKey: false},
		},
	}

	if err := SaveGlobalConfig(want); err != nil {
		t.Fatalf("SaveGlobalConfig: %v", err)
	}
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
	got, err := LoadRawGlobalConfig()
	if err != nil {
		t.Fatalf("LoadRawGlobalConfig: %v", err)
	}
	if got.DefaultProvider != want.DefaultProvider || got.Providers["local"].Model != want.Providers["local"].Model {
		t.Fatalf("saved config = %#v, want %#v", got, want)
	}
}

func TestLoadGlobalConfigRejectsInvalidProviderNames(t *testing.T) {
	for _, test := range []struct {
		name string
		key  string
	}{
		{name: "empty", key: ""},
		{name: "whitespace", key: "  "},
		{name: "colon", key: "edge:primary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setTestConfigPath(t)
			writeTestConfig(t, `{
				"default_provider": "valid",
				"providers": {`+strconv.Quote(test.key)+`: {"enabled": false}}
			}`)

			for _, load := range []struct {
				name string
				fn   func() (*GlobalConfig, error)
			}{
				{name: "raw", fn: LoadRawGlobalConfig},
				{name: "global", fn: LoadGlobalConfig},
			} {
				t.Run(load.name, func(t *testing.T) {
					if _, err := load.fn(); err == nil || !strings.Contains(err.Error(), "invalid provider name") {
						t.Fatalf("expected invalid provider-name error, got %v", err)
					}
				})
			}
		})
	}
}

func TestSaveGlobalConfigRejectsInvalidProviderNameWithoutReplacingExistingFile(t *testing.T) {
	setTestConfigPath(t)
	existing := `{"default_provider":"local","providers":{"local":{"enabled":false}}}`
	writeTestConfig(t, existing)

	err := SaveGlobalConfig(&GlobalConfig{
		DefaultProvider: "local",
		Providers: map[string]Config{
			"edge:primary": {Enabled: false},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid provider name") {
		t.Fatalf("expected invalid provider-name error, got %v", err)
	}
	got, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read existing config: %v", err)
	}
	if string(got) != existing {
		t.Fatalf("config was replaced: got %q, want %q", got, existing)
	}
}

func TestGlobalConfigAllowsHyphenatedProviderAndColonModel(t *testing.T) {
	setTestConfigPath(t)
	want := &GlobalConfig{
		DefaultProvider: "edge-primary",
		Providers: map[string]Config{
			"edge-primary": {Model: "vendor:model", Enabled: false},
		},
	}

	if err := SaveGlobalConfig(want); err != nil {
		t.Fatalf("SaveGlobalConfig: %v", err)
	}
	for _, load := range []struct {
		name string
		fn   func() (*GlobalConfig, error)
	}{
		{name: "raw", fn: LoadRawGlobalConfig},
		{name: "global", fn: LoadGlobalConfig},
	} {
		t.Run(load.name, func(t *testing.T) {
			got, err := load.fn()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if got.DefaultProvider != "edge-primary" || got.Providers["edge-primary"].Model != "vendor:model" {
				t.Fatalf("loaded config = %#v", got)
			}
		})
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
