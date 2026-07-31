package catalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dotcommander/llambo/providers"
)

const (
	fetchTimeout    = 30 * time.Second
	fetchBodyLimit  = 10 << 20 // 10 MB
	fetchErrPreview = 200      // bytes of body shown in errors
)

// UpstreamModel is a model returned by a provider's model-list API.
type UpstreamModel struct {
	ID              string
	OwnedBy         string
	UpstreamCreated time.Time // zero if unavailable
	Metadata        ModelMetadata
}

// Fetcher retrieves the upstream model list for a provider.
type Fetcher interface {
	// Fetch returns (models, endpointURL, error).
	Fetch(ctx context.Context, cfg providers.Config) ([]UpstreamModel, string, error)
}

// FetcherFor picks the right fetcher based on provider config.
func FetcherFor(cfg providers.Config) Fetcher {
	if providers.IsGeminiProvider(cfg) {
		return geminiFetcher{}
	}
	if strings.EqualFold(cfg.ProviderType, "anthropic") {
		return anthropicFetcher{}
	}
	return openaiCompatFetcher{}
}

// apiKey returns the first available key: APIKeys[0] → APIKey → EnvVar.
// Returns "" if none found and cfg does not require a key.
func apiKey(name string, cfg providers.Config) (string, error) {
	if len(cfg.APIKeys) > 0 && cfg.APIKeys[0] != "" {
		return cfg.APIKeys[0], nil
	}
	key := providers.GetAPIKey(name, cfg)
	if key != "" {
		return key, nil
	}
	if cfg.GetRequiresKey() {
		return "", fmt.Errorf("no API key configured for provider %q (set api_key, api_keys, or %s)", name, cfg.EnvVar)
	}
	return "", nil
}

// doGet executes a GET request with timeout and body cap.
// Non-2xx returns an error with status code + first fetchErrPreview bytes.
func doGet(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := body
		if len(preview) > fetchErrPreview {
			preview = preview[:fetchErrPreview]
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(preview))
	}
	return body, nil
}

// inferProviderName extracts a provider name hint from EnvVar (e.g. "OPENAI_API_KEY" → "openai").
// Used only for key lookup when no explicit name is available.
func inferProviderName(cfg providers.Config) string {
	if cfg.EnvVar == "" {
		return ""
	}
	name := strings.ToLower(strings.TrimSuffix(cfg.EnvVar, "_API_KEY"))
	return name
}

// FetchForProvider is a convenience wrapper that builds the fetcher and calls Fetch.
// The providerName is used for key resolution (GetAPIKey env-var fallback).
func FetchForProvider(ctx context.Context, name string, cfg providers.Config) ([]UpstreamModel, string, error) {
	// Patch cfg so inferProviderName / GetAPIKey can resolve by name.
	if cfg.EnvVar == "" {
		cfg.EnvVar = strings.ToUpper(name) + "_API_KEY"
	}
	return FetcherFor(cfg).Fetch(ctx, cfg)
}
