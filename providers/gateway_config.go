package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultMaxActiveJobs     = 64
	DefaultMaxRequestsPerJob = 500
)

// GatewayConfig controls gateway admission and backpressure behavior.
type GatewayConfig struct {
	MaxActiveJobs     int `json:"max_active_jobs,omitempty"`
	MaxRequestsPerJob int `json:"max_requests_per_job,omitempty"`
	// AuthToken is supported for local config files. AuthTokenEnv is preferred
	// so the secret does not need to be persisted in config.json.
	AuthToken      string   `json:"auth_token,omitempty"`
	AuthTokenEnv   string   `json:"auth_token_env,omitempty"`
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
}

// ResolveAuthToken returns the optional gateway bearer token. When an
// environment-variable name is configured it is authoritative and must exist.
func (g GatewayConfig) ResolveAuthToken() (string, error) {
	if envVar := strings.TrimSpace(g.AuthTokenEnv); envVar != "" {
		token, ok := os.LookupEnv(envVar)
		if !ok || strings.TrimSpace(token) == "" {
			return "", fmt.Errorf("gateway auth token environment variable %s is not set", envVar)
		}
		return token, nil
	}
	return g.AuthToken, nil
}

func (g *GatewayConfig) ApplyDefaults() {
	if g.MaxActiveJobs <= 0 {
		g.MaxActiveJobs = DefaultMaxActiveJobs
	}
	if g.MaxRequestsPerJob <= 0 {
		g.MaxRequestsPerJob = DefaultMaxRequestsPerJob
	}
}

// RoutingConfig controls intelligent provider selection.
type RoutingConfig struct {
	Mode             string             `json:"mode,omitempty"` // fastest|cheapest|balanced|quality
	CatalogModels    string             `json:"catalog_models,omitempty"`
	MaxCostUSD       float64            `json:"max_cost_usd,omitempty"`
	MaxLatencyMs     int                `json:"max_latency_ms,omitempty"`
	AllowedProviders []string           `json:"allowed_providers,omitempty"`
	DeniedProviders  []string           `json:"denied_providers,omitempty"`
	DailyMaxRequests map[string]int64   `json:"daily_max_requests,omitempty"`
	DailyMaxTokens   map[string]int64   `json:"daily_max_tokens,omitempty"`
	DailyMaxCostUSD  map[string]float64 `json:"daily_max_cost_usd,omitempty"`
	MetricsPath      string             `json:"metrics_path,omitempty"`
	EventsPath       string             `json:"events_path,omitempty"`
	Canary           *CanaryConfig      `json:"canary,omitempty"`
}

func (r *RoutingConfig) ApplyDefaults() {
	r.Mode = NormalizeRoutingMode(r.Mode)
	if r.MetricsPath == "" {
		r.MetricsPath = filepath.Join(configDir, "routing-metrics.json")
	}
	if r.EventsPath == "" {
		r.EventsPath = filepath.Join(configDir, "routing-events.jsonl")
	}
}
