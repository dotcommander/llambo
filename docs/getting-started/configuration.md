# Configuration Guide

This guide explains how to configure Llambo's providers and settings.

## Config File Location

Llambo's configuration is stored at:

```
~/.config/llambo/config.json
```

Create the initial configuration file with:

```bash
llambo config init
```

## Complete Example Configuration

Here's a complete example configuration showing all available fields:

```json
{
  "providers": {
    "openai": {
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "api_keys": ["sk-key1", "sk-key2", "sk-key3"],
      "max_tokens": 4096,
      "temperature": 0.7,
      "workers": 3,
      "priority": 1,
      "enabled": true,
      "env_var": "OPENAI_API_KEY",
      "requires_key": true,
      "extra_headers": {},
      "api_path": null,
      "capabilities": ["chat", "code"],
      "quality": 0.85,
      "input_cost_per_1m": 2.5,
      "output_cost_per_1m": 10,
      "expected_latency_ms": 2500
    },
    "openrouter": {
      "base_url": "https://openrouter.ai/api",
      "model": "anthropic/claude-3-5-sonnet",
      "workers": 2,
      "priority": 2,
      "enabled": true,
      "env_var": "OPENROUTER_API_KEY",
      "requires_key": true,
      "models": ["anthropic/claude-3-5-sonnet", "openai/gpt-4o-mini"],
      "extra_headers": {
        "HTTP-Referer": "https://github.com/llambo",
        "X-Title": "Llambo Gateway"
      },
      "api_path": null
    }
  }
}
```

## Configuration Fields Reference

### Required Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | - | API endpoint base URL (**without** `/v1` suffix) |
| `model` | string | - | Model identifier to use |
| `enabled` | boolean | - | Whether this backend is active |

### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `api_key` | string | (none) | Single inline API key; prefer `api_keys` or `env_var` |
| `api_keys` | array[string] | `[]` | Array of API keys (rotates on 429) |
| `env_var` | string | `{PROVIDER_NAME}_API_KEY` | Environment variable for single API key (fallback if no `api_keys`) |
| `models` | array[string] | `[]` | Optional provider model list used by catalog refresh and routing helpers |
| `workers` | integer | `2` | Parallel request slots |
| `priority` | integer | `0` | Load balancing order (lower = higher priority) |
| `max_tokens` | integer | (none) | Maximum completion tokens |
| `temperature` | number | (none) | Sampling temperature |
| `extra_headers` | object | `{}` | Provider-specific HTTP headers |
| `extra_body` | object | `{}` | Provider request body fields passed through to the underlying provider |
| `extra_body_by_model` | object | `{}` | Per-model provider request body overrides |
| `requires_key` | boolean | `true` | Whether API key is required |
| `api_path` | string | `null` | Legacy compatibility field; serving path ignores it |
| `provider_type` | string | `openai` | Provider adapter type, such as `openai`, `openrouter`, or `gemini` |
| `capabilities` | array[string] | `[]` | Routing fit labels, such as `chat`, `code`, or `extraction` |
| `quality` | number | `0.5` | Relative routing quality score from 0 to 1 |
| `input_cost_per_1m` | number | `0` | Input token cost estimate per 1M tokens |
| `output_cost_per_1m` | number | `0` | Output token cost estimate per 1M tokens |
| `expected_latency_ms` | integer | (none) | Static routing latency hint before live metrics mature |

## Key Configuration Concepts

### API Key Management

Llambo supports multiple API key management strategies:

1. **Array of keys** (`api_keys`): Multiple keys for automatic rotation on rate limits
2. **Environment variable** (`env_var`): Single key from environment
3. **No key required** (`requires_key: false`): For local models like Ollama

### Load Balancing and Parallelism

- `workers`: Controls concurrent request capacity per backend
- `priority`: Determines backend selection order (lower values = higher priority)
- Backends with same priority use round-robin selection

### API Paths

OpenAI-compatible serving normalizes `base_url` and appends the chat completion
path internally. Keep `base_url` at the provider root without `/v1`; `api_path`
is retained for older config files but is not read by the serving path.

### Provider-Specific Headers

Add custom headers with `extra_headers`:

```json
{
  "openrouter": {
    "extra_headers": {
      "HTTP-Referer": "https://myapp.com",
      "X-Title": "My Application"
    }
  }
}
```

## Common Configuration Patterns

### High Availability Setup

```json
{
  "providers": {
    "openai-primary": {
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "workers": 3,
      "priority": 1,
      "enabled": true,
      "api_keys": ["sk-key1", "sk-key2", "sk-key3"]
    },
    "openrouter-fallback": {
      "base_url": "https://openrouter.ai/api",
      "model": "anthropic/claude-3-5-sonnet",
      "workers": 2,
      "priority": 2,
      "enabled": true
    }
  }
}
```

### Local + Cloud Hybrid

```json
{
  "providers": {
    "ollama": {
      "base_url": "http://localhost:11434",
      "model": "llama3.2",
      "workers": 1,
      "priority": 1,
      "requires_key": false,
      "enabled": true
    },
    "openai": {
      "base_url": "https://api.openai.com",
      "model": "gpt-4o-mini",
      "workers": 3,
      "priority": 5,
      "enabled": true
    }
  }
}
```

## Validating Your Configuration

View your current configuration:

```bash
llambo config show
```

Test the configuration by starting the server:

```bash
llambo serve
```

Check provider status:

```bash
curl http://localhost:8080/providers
curl http://localhost:8080/health
```

## Common Issues

### Double `/v1` in URL

**Problem**: HTTP 404 errors from OpenAI-compatible providers
**Cause**: Including `/v1` suffix in `base_url`
**Solution**: Remove the suffix

```json
// Wrong
"base_url": "https://api.openai.com/v1"

// Correct
"base_url": "https://api.openai.com"
```

### Provider Not Appearing

Check:
1. `enabled: true` is set
2. API key environment variable is set (if `requires_key: true`)
3. Run `llambo config show` to verify configuration

### Rate Limits Hit Immediately

Reduce `workers` count:

```json
{
  "openai": {
    "workers": 1
  }
}
```
