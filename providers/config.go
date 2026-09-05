package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GlobalConfig holds all provider configs
type GlobalConfig struct {
	DefaultProvider string            `json:"default_provider"`
	Providers       map[string]Config `json:"providers"`
	Embed           EmbedConfig       `json:"embed,omitempty"`
	Routing         RoutingConfig     `json:"routing,omitempty"`
	Gateway         GatewayConfig     `json:"gateway,omitempty"`
	Blocklist       []string          `json:"blocklist,omitempty"`
	// MaxOutputCost is the global output-price ceiling in USD per 1M tokens.
	// Models whose known output price exceeds it are excluded from selection. 0 disables.
	MaxOutputCost float64 `json:"max_output_cost,omitempty"`
}

var configDir = filepath.Join(os.Getenv("HOME"), ".config", "llambo")
var configFile = filepath.Join(configDir, "config.json")

// SetConfigFile overrides the process-wide config file path used by CLI
// commands. It is intended for isolated eval/runtime configs, not concurrent
// mutation.
func SetConfigFile(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	configFile = path
	configDir = filepath.Dir(path)
}

// ConfigFile returns the active process config file path.
func ConfigFile() string {
	return configFile
}

// ResolveModelsDevKey returns the models.dev provider key for a llambo provider,
// defaulting to the provider name when no override is configured.
func ResolveModelsDevKey(name string, cfg Config) string {
	if cfg.ModelsDevKey != "" {
		return cfg.ModelsDevKey
	}
	if strings.EqualFold(cfg.ProviderType, "gemini") ||
		strings.Contains(strings.ToLower(cfg.BaseURL), "generativelanguage.googleapis.com") {
		return "google"
	}
	return name
}

// LoadGlobalConfig loads config from ~/.config/llambo/config.json
// Fails fast if config file is missing - run 'llambo config init' first
func LoadGlobalConfig() (*GlobalConfig, error) {
	cfg, err := loadGlobalConfig()
	if err != nil {
		return nil, err
	}

	// Apply defaults and merge env vars for API keys.
	for name, pcfg := range cfg.Providers {
		if pcfg.APIKey == "" {
			pcfg.APIKey = GetAPIKey(name, pcfg)
		}
		cfg.Providers[name] = pcfg
	}

	// Validate default_provider references an existing provider
	if _, ok := cfg.Providers[cfg.DefaultProvider]; !ok {
		return nil, fmt.Errorf("default_provider %q not found in providers (available: %s)", cfg.DefaultProvider, providerNames(cfg.Providers))
	}

	// Validate enabled providers have required fields
	for name, pcfg := range cfg.Providers {
		if !pcfg.Enabled {
			continue
		}
		if pcfg.BaseURL == "" && pcfg.NeedsBaseURL() {
			return nil, fmt.Errorf("provider %q missing base_url", name)
		}
		if pcfg.Model == "" {
			return nil, fmt.Errorf("provider %q missing model", name)
		}
		if pcfg.GetRequiresKey() && len(GetAPIKeys(name, pcfg)) == 0 {
			envVar := pcfg.EnvVar
			if envVar == "" {
				envVar = strings.ToUpper(name) + "_API_KEY"
			}
			return nil, fmt.Errorf("provider %q missing API key (set api_key, api_keys, or %s)", name, envVar)
		}
	}

	return cfg, nil
}

func loadGlobalConfig() (*GlobalConfig, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found: %s\nRun 'llambo config init' to create it", configFile)
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &GlobalConfig{
		Providers: make(map[string]Config),
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := validateProviderNames(cfg.Providers); err != nil {
		return nil, err
	}

	if cfg.DefaultProvider == "" {
		return nil, fmt.Errorf("config missing required field: default_provider")
	}

	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("config has no providers defined")
	}

	cfg.Routing.ApplyDefaults()
	cfg.Gateway.ApplyDefaults()

	for name, pcfg := range cfg.Providers {
		pcfg.ApplyDefaults()
		cfg.Providers[name] = pcfg
	}

	return cfg, nil
}

// LoadRawGlobalConfig loads config without merging environment API keys. Use it
// when writing derived config files so secrets from the process environment are
// not persisted accidentally.
func LoadRawGlobalConfig() (*GlobalConfig, error) {
	return loadGlobalConfig()
}

// SaveGlobalConfig saves config to ~/.config/llambo/config.json
func SaveGlobalConfig(cfg *GlobalConfig) error {
	return writeGlobalConfig(cfg)
}

func writeGlobalConfig(cfg *GlobalConfig) error {
	if cfg != nil {
		if err := validateProviderNames(cfg.Providers); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	tmp := configFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, configFile)
}

func validateProviderNames(configs map[string]Config) error {
	for name := range configs {
		if strings.TrimSpace(name) == "" || strings.Contains(name, ":") {
			return fmt.Errorf("invalid provider name: names must be non-empty and contain no ':'")
		}
	}
	return nil
}

// InitDefaultConfig creates a skeleton config file with example providers
// Users can add/remove providers freely - provider list comes entirely from config.json
func InitDefaultConfig() error {
	providers := map[string]Config{
		"openai": {
			ProviderType: "openai",
			BaseURL:      "https://api.openai.com",
			Model:        "gpt-5.1-mini",
			MaxTokens:    8192,
			Workers:      2,
			Priority:     1,
			Enabled:      true,
			RequiresKey:  true,
			APIKey:       "YOUR_OPENAI_API_KEY",
		},
		"openrouter": {
			ProviderType: "openrouter",
			BaseURL:      "https://openrouter.ai/api",
			Model:        "anthropic/claude-3.5-sonnet",
			MaxTokens:    8192,
			Workers:      2,
			Priority:     2,
			Enabled:      false,
			RequiresKey:  true,
			APIKey:       "YOUR_OPENROUTER_API_KEY",
			ExtraHeaders: map[string]string{"HTTP-Referer": "https://github.com/llambo"},
		},
		"lmstudio": {
			ProviderType: "openai",
			BaseURL:      "http://localhost:1234",
			Model:        "local-model",
			MaxTokens:    4096,
			Workers:      2,
			Priority:     10,
			Enabled:      false,
			RequiresKey:  false,
		},
		"synthetic": {
			ProviderType: "openai",
			BaseURL:      "https://api.synthetic.new",
			Model:        "hf:zai-org/glm-4.7",
			MaxTokens:    8192,
			Workers:      2,
			Priority:     3,
			Enabled:      false,
			RequiresKey:  true,
			APIKey:       "YOUR_SYNTHETIC_API_KEY",
		},
		"zai": {
			ProviderType: "openai",
			BaseURL:      "https://api.z.ai/api/coding/paas/v4",
			Model:        "GLM-5.2",
			MaxTokens:    8192,
			Workers:      2,
			Priority:     4,
			Enabled:      false,
			RequiresKey:  true,
			APIKey:       "YOUR_ZAI_API_KEY",
		},
	}

	cfg := &GlobalConfig{
		DefaultProvider: "openai",
		Providers:       providers,
		Routing: RoutingConfig{
			Mode:        string(RoutingModeBalanced),
			MetricsPath: filepath.Join(configDir, "routing-metrics.json"),
			EventsPath:  filepath.Join(configDir, "routing-events.jsonl"),
		},
		Gateway: GatewayConfig{
			MaxActiveJobs:     DefaultMaxActiveJobs,
			MaxRequestsPerJob: DefaultMaxRequestsPerJob,
		},
	}

	return writeGlobalConfig(cfg)
}
