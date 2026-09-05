package cmd

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/llambo/providers"
	"gopkg.in/yaml.v3"
)

//go:embed route_preferences.default.yaml
var defaultRoutePreferencesYAML []byte

type routePreferenceSet struct {
	Preferences     []routePreference `yaml:"route_preferences" json:"route_preferences"`
	FallbackDecider routeModelChoice  `yaml:"fallback_decider" json:"fallback_decider"`
	Threshold       float64           `yaml:"threshold,omitempty" json:"threshold,omitempty"`
	FallbackMode    string            `yaml:"fallback_mode,omitempty" json:"fallback_mode,omitempty"`
}

type routePreference struct {
	Name   string               `yaml:"name" json:"name"`
	Match  routePreferenceMatch `yaml:"match" json:"match"`
	Choose routeModelChoice     `yaml:"choose" json:"choose"`
	Reason string               `yaml:"reason,omitempty" json:"reason,omitempty"`
}

type routePreferenceMatch struct {
	Intent     string   `yaml:"intent,omitempty" json:"intent,omitempty"`
	AnyPhrases []string `yaml:"any_phrases,omitempty" json:"any_phrases,omitempty"`
	Default    bool     `yaml:"default,omitempty" json:"default,omitempty"`
}

type routeModelChoice struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
	Reason   string `yaml:"reason,omitempty" json:"reason,omitempty"`
}

type routeQueryResult struct {
	Intent   providers.RoutingIntent
	Provider string
	Model    string
	Reason   string
}

func routePreferencesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "llambo", "route-preferences.yaml"), nil
}

func loadRoutePreferences(path string) (routePreferenceSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			data = defaultRoutePreferencesYAML
		} else {
			return routePreferenceSet{}, fmt.Errorf("read route preferences: %w", err)
		}
	}
	return parseRoutePreferences(data)
}

func parseRoutePreferences(data []byte) (routePreferenceSet, error) {
	var prefs routePreferenceSet
	if err := yaml.Unmarshal(data, &prefs); err != nil {
		return routePreferenceSet{}, fmt.Errorf("parse route preferences: %w", err)
	}
	if strings.TrimSpace(prefs.FallbackMode) == "" {
		prefs.FallbackMode = string(providers.RoutingModeQuality)
	}
	return prefs, nil
}

func writeDefaultRoutePreferences(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat route preferences: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create route preferences dir: %w", err)
	}
	if err := os.WriteFile(path, defaultRoutePreferencesYAML, 0o644); err != nil {
		return fmt.Errorf("write route preferences: %w", err)
	}
	return nil
}

func routeQuery(query string, configs map[string]providers.Config, routing providers.RoutingConfig, metrics map[string]providers.ProviderRuntimeMetrics, prefs routePreferenceSet) (routeQueryResult, error) {
	intent := providers.DetectIntent("", query)
	if result, ok := matchRoutePreference(query, intent, configs, prefs.Preferences); ok {
		return result, nil
	}
	if result, ok := matchRouteChoice(intent, configs, prefs.FallbackDecider); ok {
		return result, nil
	}

	routing.Mode = routeFallbackMode(prefs)
	decision := providers.SimulateRoute(configs, routing, metrics, intent, providers.EstimatePromptTokens("", query))
	if decision.Chosen == "" {
		return routeQueryResult{}, fmt.Errorf("no route candidates available for intent %q", intent)
	}

	cfg := configs[decision.Chosen]
	return routeQueryResult{
		Intent:   intent,
		Provider: providers.BaseProviderName(decision.Chosen, cfg.Model),
		Model:    cfg.Model,
		Reason:   decision.Reason,
	}, nil
}

func matchRoutePreference(query string, intent providers.RoutingIntent, configs map[string]providers.Config, prefs []routePreference) (routeQueryResult, bool) {
	for _, pref := range prefs {
		if !routePreferenceMatches(query, intent, pref.Match) {
			continue
		}
		result, ok := matchRouteChoice(intent, configs, pref.Choose)
		if !ok {
			continue
		}
		if strings.TrimSpace(pref.Reason) != "" {
			result.Reason = pref.Reason
		}
		return result, true
	}
	return routeQueryResult{}, false
}

func routePreferenceMatches(query string, intent providers.RoutingIntent, match routePreferenceMatch) bool {
	if match.Default {
		return true
	}
	if strings.TrimSpace(match.Intent) != "" && !strings.EqualFold(match.Intent, string(intent)) {
		return false
	}
	if len(match.AnyPhrases) == 0 {
		return strings.TrimSpace(match.Intent) != ""
	}
	queryLower := strings.ToLower(query)
	for _, phrase := range match.AnyPhrases {
		if strings.Contains(queryLower, strings.ToLower(strings.TrimSpace(phrase))) {
			return true
		}
	}
	return false
}

func matchRouteChoice(intent providers.RoutingIntent, configs map[string]providers.Config, choice routeModelChoice) (routeQueryResult, bool) {
	provider, model := findConfiguredRouteModel(configs, choice.Provider, choice.Model)
	if provider == "" {
		return routeQueryResult{}, false
	}
	return routeQueryResult{
		Intent:   intent,
		Provider: provider,
		Model:    model,
		Reason:   choice.Reason,
	}, true
}

func routeFallbackMode(prefs routePreferenceSet) string {
	return providers.NormalizeRoutingMode(prefs.FallbackMode)
}

func findConfiguredRouteModel(configs map[string]providers.Config, provider, model string) (string, string) {
	for name, cfg := range configs {
		if !strings.EqualFold(providers.BaseProviderName(name, cfg.Model), provider) {
			continue
		}
		if strings.EqualFold(cfg.Model, model) {
			return providers.BaseProviderName(name, cfg.Model), cfg.Model
		}
	}
	return "", ""
}
