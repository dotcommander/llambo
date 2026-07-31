# Configuration Guide

Complete guide to configuring Llambo.

## Config File Location

```
~/.config/llambo/config.json
```

Create with: `llambo config init`

## Full Example

```json
{
  "providers": {
    "openai": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "max_tokens": 4096,
      "temperature": 0.7,
      "workers": 3,
      "priority": 1,
      "enabled": true,
      "env_var": "OPENAI_API_KEY",
      "requires_key": true
    },
    "openrouter": {
      "provider_type": "openrouter",
      "base_url": "https://openrouter.ai/api",
      "model": "anthropic/claude-3-5-sonnet",
      "workers": 2,
      "priority": 2,
      "enabled": true,
      "requires_key": true,
      "extra_headers": {
        "HTTP-Referer": "https://github.com/llambo",
        "X-Title": "Llambo Gateway"
      }
    },
    "gemini": {
      "provider_type": "gemini",
      "base_url": "https://generativelanguage.googleapis.com/v1beta",
      "model": "gemini-2.0-flash-exp",
      "workers": 2,
      "priority": 3,
      "enabled": false,
      "requires_key": true
    },
    "local-lmstudio": {
      "provider_type": "openai",
      "base_url": "http://localhost:1234",
      "model": "local-model",
      "workers": 1,
      "priority": 10,
      "enabled": true,
      "requires_key": false
    },
    "custom-provider": {
      "provider_type": "openai",
      "base_url": "https://api.custom.ai",
      "model": "custom-model",
      "workers": 2,
      "enabled": true,
      "requires_key": true,
      "env_var": "CUSTOM_AI_KEY",
      "models": ["custom-model", "custom-fast"],
      "capabilities": ["chat", "extraction"],
      "quality": 0.7,
      "input_cost_per_1m": 0.5,
      "output_cost_per_1m": 1.5,
      "expected_latency_ms": 1800
    }
  }
}
```

## Provider Configuration

### Required Fields

| Field | Description |
|-------|-------------|
| `base_url` | API endpoint base URL (**without** `/v1` suffix) |
| `model` | Model identifier to use |
| `enabled` | Whether this backend is active |

### Optional Fields

| Field | Default | Description |
|-------|---------|-------------|
| `provider_type` | `openai` | Provider type identifier |
| `api_key` | (none) | Single inline API key; prefer `api_keys` or `env_var` |
| `api_keys` | `[]` | Multiple API keys with rotation on 429 |
| `workers` | `2` | Concurrent request slots |
| `priority` | `0` | Load balancing order (lower = higher priority) |
| `max_tokens` | (none) | Maximum completion tokens |
| `temperature` | (none) | Sampling temperature |
| `env_var` | `{NAME}_API_KEY` | Environment variable for API key |
| `requires_key` | `true` | Whether API key is required |
| `models` | `[]` | Optional provider model list used by catalog refresh and routing helpers |
| `extra_headers` | (none) | Additional HTTP headers |
| `extra_body` | `{}` | Provider request body fields passed through to the underlying provider |
| `extra_body_by_model` | `{}` | Per-model provider request body overrides |
| `api_path` | (none) | Legacy compatibility field; serving path ignores it |
| `capabilities` | `[]` | Routing fit labels, such as `chat`, `code`, or `extraction` |
| `quality` | `0.5` | Relative routing quality score from 0 to 1 |
| `input_cost_per_1m` | `0` | Input token cost estimate per 1M tokens |
| `output_cost_per_1m` | `0` | Output token cost estimate per 1M tokens |
| `expected_latency_ms` | (none) | Static routing latency hint before live metrics mature |

## Provider Types

### OpenAI-Compatible (`provider_type: "openai"`)

Works with:
- OpenAI API
- Azure OpenAI
- Local models (Ollama, LM Studio, etc.)
- Any OpenAI-compatible API

```json
{
  "openai": {
    "provider_type": "openai",
    "base_url": "https://api.openai.com",
    "model": "gpt-4o",
    "enabled": true,
    "requires_key": true
  }
}
```

### OpenRouter (`provider_type: "openrouter"`)

```json
{
  "openrouter": {
    "provider_type": "openrouter",
    "base_url": "https://openrouter.ai/api",
    "model": "anthropic/claude-3-5-sonnet",
    "enabled": true,
    "requires_key": true,
    "extra_headers": {
      "HTTP-Referer": "https://yoursite.com"
    }
  }
}
```

**Note:** OpenRouter requires `HTTP-Referer` header.

OpenRouter-specific request controls can be passed through with `extra_body`.
Use this for provider routing preferences, data-policy constraints, reasoning
controls, transforms, and similar OpenRouter request-body fields:

```json
{
  "openrouter": {
    "provider_type": "openrouter",
    "base_url": "https://openrouter.ai/api",
    "model": "qwen/qwen3-30b-a3b-instruct-2507",
    "enabled": true,
    "requires_key": true,
    "extra_headers": {
      "HTTP-Referer": "https://yoursite.com",
      "X-Title": "Llambo Gateway"
    },
    "extra_body": {
      "provider": {
        "sort": "throughput",
        "allow_fallbacks": false,
        "require_parameters": true,
        "data_collection": "deny"
      },
      "reasoning": {
        "effort": "minimal"
      }
    },
    "extra_body_by_model": {
      "qwen/qwen3-235b-a22b-thinking-2507": {
        "reasoning": {"effort": "high"}
      }
    }
  }
}
```

Keep cross-provider routing policy in Llambo when you need Llambo's metrics,
quotas, key rotation, and circuit breakers. Use OpenRouter's `provider` object
for endpoint-level preferences inside the OpenRouter backend.

### Gemini (`provider_type: "gemini"`)

```json
{
  "gemini": {
    "provider_type": "gemini",
    "base_url": "https://generativelanguage.googleapis.com/v1beta",
    "model": "gemini-2.0-flash-exp",
    "enabled": true,
    "requires_key": true
  }
}
```

## API Keys

### Default Resolution

By default, Llambo looks for `{PROVIDER_NAME}_API_KEY`:

| Provider Name | Environment Variable |
|---------------|---------------------|
| `openai` | `OPENAI_API_KEY` |
| `openrouter` | `OPENROUTER_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `custom-name` | `CUSTOM-NAME_API_KEY` |

### Custom Environment Variable

Override with `env_var`:

```json
{
  "my-openai": {
    "base_url": "https://api.openai.com",
    "model": "gpt-4o",
    "env_var": "MY_OPENAI_KEY",
    "enabled": true
  }
}
```

### No API Key Required

For local models:

```json
{
  "ollama": {
    "base_url": "http://localhost:11434",
    "model": "llama3.2",
    "requires_key": false,
    "enabled": true
  }
}
```

## Load Balancing

### Workers

`workers` controls concurrent request capacity per backend:

```json
{
  "openai": { "workers": 5 },
  "openrouter": { "workers": 2 }
}
```

Higher worker count = more parallel requests to that backend.

### Priority

`priority` controls backend selection order (lower = higher priority):

```json
{
  "primary": { "priority": 1 },
  "fallback": { "priority": 10 }
}
```

Backends with same priority use round-robin selection.

### Effective Load Distribution

With this config:

```json
{
  "fast-backend": { "workers": 4, "priority": 1 },
  "slow-backend": { "workers": 2, "priority": 2 }
}
```

- `fast-backend` handles requests first (priority 1)
- When `fast-backend` is at capacity, `slow-backend` gets requests
- Job batches distribute: 4 concurrent to fast, 2 to slow

## API Paths

OpenAI-compatible serving normalizes `base_url` and appends the chat completion
path internally. Keep `base_url` at the provider root without `/v1`; `api_path`
is retained for older config files but is not read by the serving path.

## Extra Headers

Provider-specific headers:

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

## Common Configurations

### High Availability

Multiple providers with failover:

```json
{
  "providers": {
    "openai-primary": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "workers": 3,
      "priority": 1,
      "enabled": true,
      "requires_key": true
    },
    "openrouter-fallback": {
      "provider_type": "openrouter",
      "base_url": "https://openrouter.ai/api",
      "model": "openai/gpt-4o",
      "workers": 2,
      "priority": 2,
      "enabled": true,
      "requires_key": true
    }
  }
}
```

### Maximum Throughput

High worker counts across multiple backends:

```json
{
  "providers": {
    "openai": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o-mini",
      "workers": 10,
      "priority": 1,
      "enabled": true,
      "requires_key": true
    },
    "openrouter": {
      "provider_type": "openrouter",
      "base_url": "https://openrouter.ai/api",
      "model": "openai/gpt-4o-mini",
      "workers": 10,
      "priority": 1,
      "enabled": true,
      "requires_key": true
    }
  }
}
```

### Cost Optimization

Prefer cheaper models, fallback to premium:

```json
{
  "providers": {
    "cheap": {
      "provider_type": "openrouter",
      "base_url": "https://openrouter.ai/api",
      "model": "meta-llama/llama-3.1-8b-instruct",
      "workers": 5,
      "priority": 1,
      "enabled": true,
      "requires_key": true
    },
    "premium": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "workers": 2,
      "priority": 10,
      "enabled": true,
      "requires_key": true
    }
  }
}
```

## Routing and Cost Controls

`routing.mode` controls model selection policy:

- `fastest`: prioritize observed or configured latency
- `cheapest`: prioritize estimated token cost
- `balanced`: blend quality, task fit, latency, cost, and reliability
- `quality`: prioritize configured quality and task fit

Catalog-backed routing is opt-in:

```json
{
  "routing": {
    "mode": "balanced",
    "catalog_models": "pinned",
    "metrics_path": "~/.config/llambo/routing-metrics.json",
    "events_path": "~/.config/llambo/routing-events.jsonl",
    "daily_max_requests": {"groq": 200},
    "daily_max_tokens": {"*": 300000},
    "daily_max_cost_usd": {"*": 25.0}
  }
}
```

`catalog_models: "pinned"` turns each pinned, non-avoided catalog model into a
routing backend using that provider's credentials. Price estimates come from
provider config and `~/.config/llambo/model-costs.csv`; a model is considered
free only when both input and output prices are explicitly `0`.

Useful checks:

```bash
llambo providers refresh nvidia
llambo models catalog --free
llambo models catalog openrouter --metadata
llambo models discover-free -P nvidia --pin
llambo ping --models healthy -P nvidia
llambo route simulate --modes fastest,cheapest,balanced,quality
```

If you have task-specific benchmark evidence, import it into the catalog as JSON:

```json
[
  {
    "provider": "openrouter",
    "model": "qwen/qwen3-30b-a3b-instruct-2507",
    "task": "extraction",
    "score": 1.0,
    "source": "distill 86-fact benchmark"
  }
]
```

```bash
llambo models catalog import-quality ./quality.json
```

Imported task names are added as catalog tags. When `routing.catalog_models` is
`pinned`, pinned model backends inherit the best imported quality score and task
capability from their catalog entry.

### Local + Cloud Hybrid

Use local models when available, cloud as fallback:

```json
{
  "providers": {
    "ollama": {
      "provider_type": "openai",
      "base_url": "http://localhost:11434",
      "model": "llama3.2",
      "workers": 1,
      "priority": 1,
      "requires_key": false,
      "enabled": true
    },
    "openai": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o-mini",
      "workers": 3,
      "priority": 5,
      "enabled": true,
      "requires_key": true
    }
  }
}
```

## Validation

View current config:

```bash
llambo config show
```

Test configuration by starting server:

```bash
llambo serve
```

Check provider health:

```bash
curl http://localhost:8080/providers
curl http://localhost:8080/health
```

## Common Issues

### "invalid base_url" Error

**Cause:** Including `/v1` suffix in base_url.

**Fix:** Remove the suffix:

```json
// Wrong
"base_url": "https://api.openai.com/v1"

// Correct
"base_url": "https://api.openai.com"
```

### Provider Not Appearing

Check:
1. `enabled: true` is set
2. API key environment variable is set
3. Run `llambo config show` to verify

### Rate Limits Hit Immediately

Reduce `workers` count:

```json
{
  "openai": {
    "workers": 1
  }
}
```
