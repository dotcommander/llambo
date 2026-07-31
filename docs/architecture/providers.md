# Provider System

Llambo's provider system provides a unified interface to multiple LLM backends with automatic failover, key rotation, and circuit breaker protection.

## Overview

The provider system abstracts LLM APIs through a clean interface that supports:
- Multiple provider backends (OpenAI, OpenRouter, Gemini, etc.)
- Per-backend client isolation for correct routing
- Automatic failover on errors
- Key rotation for rate limit handling
- Circuit breaker health tracking
- Configuration-driven provider management

## Core Interfaces

### Provider Interface

The base provider interface defines the essential contract for LLM backends:

```go
// Provider interface for AI backends
type Provider interface {
    Name() string
    Chat(systemPrompt, userContent string) (string, error)
    MaxTokens() int
}
```

### ProviderWithInfo Interface

Extended interface that includes detailed response metadata:

```go
type ProviderWithInfo interface {
    Provider
    ChatWithInfo(systemPrompt, userContent string) (ChatResult, error)
}
```

### ChatProvider Interface

The primary interface used by the gateway, supporting context and failover:

```go
type ChatProvider interface {
    Provider
    ChatWithInfo(systemPrompt, userContent string) (ChatResult, error)
    ChatWithInfoContext(ctx context.Context, systemPrompt, userContent string) (ChatResult, error)
    GetOpenAIClients() *OpenAIClients
    Shutdown()
}
```

## ChatResult Structure

Responses include metadata for provider transparency:

```go
type ChatResult struct {
    Content  string    // Response content
    Provider string    // Backend that served the request
    Model    string    // Model used for completion
    Usage    *LLMUsage // Token usage and cost (may be nil)
}
```

This structure enables response header injection (`X-Llambo-Provider`, `X-Llambo-Model`) and cost tracking.

## Per-Backend Client Creation

Each backend gets its own dedicated OpenAI client to ensure correct BaseURL routing:

### Client Factory Pattern

The `CreateOpenAIClients` function in `client_factory.go` creates isolated clients:

```go
// CreateOpenAIClients creates OpenAI clients for all enabled backends.
// Each backend gets its own OpenAI client to ensure correct BaseURL routing.
func CreateOpenAIClients(configs map[string]Config) (*OpenAIClients, error) {
    clients := make(map[string]openai.Client)
    var backends []Backend

    for _, entry := range FilterEnabledProviders(configs) {
        // Create dedicated client for this backend
        client, err := createClientForConfigWithKey(
            entry.Name,
            entry.Config,
            keyRotator.GetKey(entry.Name)
        )
        if err != nil {
            return nil, fmt.Errorf("openai client init for %s: %w", entry.Name, err)
        }

        clients[entry.Name] = client
        backends = append(backends, Backend{
            Name:  entry.Name,
            Model: entry.Config.Model
        })
    }

    return &OpenAIClients{
        Clients:        clients,
        Backends:       backends,
        CircuitBreaker: NewCircuitBreaker(backendNames, callback),
        CostTracker:    NewCostTracker(backendNames),
        KeyRotator:     keyRotator,
        configs:        configs,
    }, nil
}
```

### Key Benefits

1. **Isolated Routing**: Each client has its own BaseURL configuration
2. **Independent Failover**: Circuit breaker tracks health per backend
3. **Key Management**: Each backend manages its own API key rotation
4. **Cost Tracking**: Token usage and costs tracked per provider

### Client Configuration

The `createClientForConfigWithKey` function handles client initialization:

```go
func createClientForConfigWithKey(name string, cfg Config, apiKey string) (openai.Client, error) {
    var opts []option.RequestOption

    if apiKey != "" {
        opts = append(opts, option.WithAPIKey(apiKey))
    }

    // Set base URL (ensure it ends with /v1 for OpenAI SDK compatibility)
    baseURL := normalizeBaseURL(cfg.BaseURL)
    if baseURL != "" {
        opts = append(opts, option.WithBaseURL(baseURL))
    }

    // Add provider-specific headers
    for key, value := range cfg.ExtraHeaders {
        opts = append(opts, option.WithHeader(key, value))
    }

    return openai.NewClient(opts...), nil
}
```

## Configuration-Driven Architecture

### Provider Configuration Structure

Providers are defined entirely in `~/.config/llambo/config.json`:

```json
{
  "providers": {
    "openai": {
      "base_url": "https://api.openai.com",
      "model": "gpt-5.1-mini",
      "api_keys": ["sk-key1", "sk-key2", "sk-key3"],
      "max_tokens": 8192,
      "workers": 2,
      "priority": 1,
      "enabled": true,
      "requires_key": true
    },
    "openrouter": {
      "base_url": "https://openrouter.ai/api",
      "model": "anthropic/claude-3.5-sonnet",
      "workers": 2,
      "priority": 2,
      "enabled": true,
      "requires_key": true,
      "extra_headers": {
        "HTTP-Referer": "https://github.com/dotcommander/llambo"
      }
    }
  }
}
```

### Configuration Fields

| Field | Required | Description |
|-------|----------|-------------|
| `base_url` | Yes | API endpoint (NO `/v1` suffix) |
| `model` | Yes | Model identifier |
| `enabled` | Yes | Whether backend is active |
| `api_key` | No | Single inline API key; prefer `api_keys` or `env_var` |
| `api_keys` | No | Array of API keys (rotates on 429) |
| `env_var` | No | Env var for single API key |
| `workers` | No | Parallel request slots (default: 2) |
| `priority` | No | Load balancing order (lower = higher priority) |
| `max_tokens` | No | Max completion tokens |
| `extra_headers` | No | Provider-specific headers |
| `requires_key` | No | Whether API key required (default: true) |
| `api_path` | No | Legacy compatibility field; serving path ignores it |

### Adding a New Provider (Config-Only)

To add a new provider, simply edit `config.json`:

1. **Create configuration entry**:

```json
"myprovider": {
  "base_url": "https://api.myprovider.com",
  "model": "my-best-model",
  "workers": 2,
  "priority": 3,
  "enabled": true,
  "requires_key": true,
  "api_key": "YOUR_API_KEY"
}
```

2. **Run the gateway**:

```bash
llambo serve
```

3. **The provider is automatically available** through all API endpoints.

### Default Configuration Generation

The `InitDefaultConfig()` function creates a skeleton config with example providers:

```go
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
        "openrouter": { /* ... */ },
        "lmstudio": { /* ... */ },
        "synthetic": { /* ... */ },
        "zai": { /* ... */ }
    }

    cfg := &GlobalConfig{
        DefaultProvider: "openai",
        Providers:       providers,
    }

    return SaveGlobalConfig(cfg)
}
```

## Load Balancing and Failover

### Priority-Based Load Balancing

Providers are ordered by priority (lower number = higher priority):

```go
// FilterEnabledProviders returns enabled providers in priority order
func FilterEnabledProviders(configs map[string]Config) []ProviderEntry {
    var entries []ProviderEntry
    for _, name := range GetProviderOrderFromConfigs(configs) {
        cfg, ok := configs[name]
        if !ok || !cfg.Enabled {
            continue
        }
        entries = append(entries, ProviderEntry{Name: name, Config: cfg})
    }
    return entries
}
```

### Worker-Based Distribution

Each provider's `workers` field determines its share of requests:

```go
type providerInfo struct {
    name   string
    cfg    Config
    weight int // workers = weight for load distribution
}
```

Higher worker counts = more request capacity allocated to that provider.

## Key Rotation and Circuit Breaker

### Multi-Key Rotation

When `api_keys` array is configured, keys rotate on HTTP 429:

```go
// RotateKey attempts to rotate to the next API key for a provider after a 429.
func (oc *OpenAIClients) RotateKey(provider string) bool {
    // Only rotate if there are multiple keys
    if oc.KeyRotator.KeyCount(provider) <= 1 {
        return false
    }

    // Mark current key as rate-limited
    if !oc.KeyRotator.MarkRateLimited(provider) {
        return false // all keys exhausted
    }

    // Get the new key and create a new client
    newKey := oc.KeyRotator.GetKey(provider)
    cfg := oc.configs[provider]

    client, err := createClientForConfigWithKey(provider, cfg, newKey)
    if err != nil {
        return false
    }

    oc.Clients[provider] = client
    return true
}
```

### Circuit Breaker Protection

Each backend has independent health tracking:

| Trigger | Action | Cooldown |
|---------|--------|----------|
| HTTP 429 (no keys left) | Immediate disable | 5 minutes |
| "rate limit" in error | Immediate disable | 5 minutes |
| "quota" in error | Immediate disable | 5 minutes |
| 3+ consecutive failures | Disable | 60 seconds |
| Success after cooldown | Re-enable, reset counter | - |

## Provider Types

### OpenAI-Compatible Providers

Providers using OpenAI SDK format (most providers):

```json
{
  "provider_type": "openai",
  "base_url": "https://api.openai.com"
}
```

### Native Gemini API

Special handling for Google's Gemini API:

```json
{
  "provider_type": "gemini",
  "base_url": "https://generativelanguage.googleapis.com"
}
```

### API Paths

OpenAI-compatible serving normalizes `base_url` and appends the chat completion
path internally. Keep `base_url` at the provider root without `/v1`; `api_path`
is retained for older config files but is not read by the serving path.

## File Structure

```
providers/
├── provider.go              # Core interfaces and types
├── config.go               # Configuration loading and defaults
├── client_factory.go       # Per-backend client creation
├── openai_provider.go      # Main provider implementation
├── circuit_breaker.go      # Health tracking
├── key_rotator.go         # Multi-key management
├── cost_tracker.go        # Token usage and cost tracking
├── queue.go               # Parallel job processing
├── embeddings.go          # Embedding provider
├── gemini_api.go          # Native Gemini integration
└── registry.go           # Provider discovery
```

## Summary

The provider system implements a **configuration-driven architecture** where:
1. **Adding providers requires only config changes** - no code modifications
2. **Each backend gets isolated clients** for correct routing
3. **Automatic failover and key rotation** handle rate limits
4. **Circuit breaker protects** against unhealthy backends
5. **Load balancing distributes** requests based on priority and workers

This design enables rapid provider integration while maintaining robust error handling and performance characteristics.
