# Multi-Provider Setup Guide

This guide explains how to configure Llambo for multi-provider operation, enabling automatic failover, load balancing, and parallel processing across multiple AI backends.

## Overview

Llambo supports running multiple providers simultaneously with:

- **Automatic failover**: When one provider fails or hits rate limits, traffic automatically shifts to healthy backends
- **Load balancing**: Requests distributed based on provider priority and available capacity
- **Parallel processing**: Batch jobs processed simultaneously across all healthy backends
- **Cost optimization**: Mix cheap and expensive models with priority-based routing

## Basic Multi-Provider Configuration

Here's a complete example configuring 2+ providers with failover:

```json
{
  "default_provider": "openai",
  "providers": {
    "openai": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "workers": 3,
      "priority": 1,
      "enabled": true,
      "requires_key": true,
      "api_keys": ["sk-key1", "sk-key2", "sk-key3"]
    },
    "openrouter": {
      "provider_type": "openrouter",
      "base_url": "https://openrouter.ai/api",
      "model": "anthropic/claude-3.5-sonnet",
      "workers": 2,
      "priority": 2,
      "enabled": true,
      "requires_key": true,
      "extra_headers": {
        "HTTP-Referer": "https://github.com/dotcommander/llambo",
        "X-Title": "Llambo Gateway"
      }
    },
    "gemini": {
      "provider_type": "gemini",
      "base_url": "https://generativelanguage.googleapis.com/v1beta",
      "model": "gemini-2.0-flash-exp",
      "workers": 2,
      "priority": 3,
      "enabled": true,
      "requires_key": true
    }
  }
}
```

## Priority-Based Load Balancing

The `priority` field controls the automatic failover chain:

| Priority | Behavior |
|----------|----------|
| **Lower values** = higher priority | First choice for requests |
| **Higher values** = lower priority | Fallback when higher-priority providers fail |

### How Priority Works

1. **Provider selection**: Llambo selects providers in priority order (lowest to highest)
2. **Healthy checks**: Only healthy providers (not circuit-broken) are considered
3. **Capacity-based routing**: Within a priority level, requests are round-robin distributed based on `workers` capacity

### Example: Three-Tier Failover Chain

```json
{
  "primary": { "priority": 1, "workers": 3 },
  "secondary": { "priority": 2, "workers": 2 },
  "tertiary": { "priority": 10, "workers": 1 }
}
```

**Request flow:**
1. All requests first go to `primary` (priority 1)
2. If `primary` fails or is at capacity, requests go to `secondary` (priority 2)
3. Only when both fail do requests go to `tertiary` (priority 10)
4. As providers recover, traffic automatically shifts back to higher-priority backends

### Same-Priority Round-Robin

Providers with equal priority share load using round-robin:

```json
{
  "openai-east": { "priority": 1, "workers": 3 },
  "openai-west": { "priority": 1, "workers": 3 }  // Same priority = load shared
}
```

## Automatic Failover Chain

### How Failover Works

Llambo implements a sophisticated failover system:

1. **Circuit breaker monitoring**: Each backend tracked individually
2. **Health-based routing**: Only healthy backends receive new requests
3. **Automatic recovery**: Failed backends automatically re-enter rotation after cooldown

### Failover Triggers

| Trigger | Action |
|---------|--------|
| HTTP 429 (rate limit) | Circuit-break provider for 5 minutes |
| "rate limit" in error message | Circuit-break provider for 5 minutes |
| "quota" in error message | Circuit-break provider for 5 minutes |
| 3+ consecutive failures | Disable for 60 seconds |
| Network timeout | Immediate failover to next healthy provider |
| Success after cooldown | Provider re-enabled automatically |

### API Key Rotation Enhancement

With multiple API keys per provider, you get extended capacity before failover:

```json
{
  "openai": {
    "api_keys": ["sk-key1", "sk-key2", "sk-key3"],  // 3 keys = 3x rate limit
    "workers": 3,
    "priority": 1
  }
}
```

**Key rotation flow:**
1. First request uses `sk-key1`
2. On 429, rotate to `sk-key2` and retry immediately (same provider)
3. Continue rotating through all keys before circuit-breaking
4. Key cooldown: exhausted keys become available after 5 minutes

## Complete Multi-Provider Examples

### High Availability Setup

```json
{
  "default_provider": "openai-primary",
  "providers": {
    "openai-primary": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "workers": 3,
      "priority": 1,
      "enabled": true,
      "requires_key": true,
      "api_keys": ["sk-key1", "sk-key2", "sk-key3"]
    },
    "openrouter-fallback": {
      "provider_type": "openrouter",
      "base_url": "https://openrouter.ai/api",
      "model": "anthropic/claude-3.5-sonnet",
      "workers": 2,
      "priority": 2,
      "enabled": true,
      "requires_key": true
    },
    "gemini-backup": {
      "provider_type": "gemini",
      "base_url": "https://generativelanguage.googleapis.com/v1beta",
      "model": "gemini-2.0-flash-exp",
      "workers": 2,
      "priority": 3,
      "enabled": true,
      "requires_key": true
    }
  }
}
```

### Cost-Optimized Setup

```json
{
  "default_provider": "cheap-llama",
  "providers": {
    "cheap-llama": {
      "provider_type": "openrouter",
      "base_url": "https://openrouter.ai/api",
      "model": "meta-llama/llama-3.1-8b-instruct",
      "workers": 5,
      "priority": 1,
      "enabled": true,
      "requires_key": true
    },
    "mid-range": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o-mini",
      "workers": 3,
      "priority": 2,
      "enabled": true,
      "requires_key": true
    },
    "premium": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "workers": 1,
      "priority": 10,
      "enabled": true,
      "requires_key": true
    }
  }
}
```

### Local + Cloud Hybrid

```json
{
  "default_provider": "ollama-local",
  "providers": {
    "ollama-local": {
      "provider_type": "openai",
      "base_url": "http://localhost:11434",
      "model": "llama3.2",
      "workers": 1,
      "priority": 1,
      "enabled": true,
      "requires_key": false
    },
    "cloud-fallback": {
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

## Parallel Job Processing

Batch jobs automatically distribute across all healthy backends:

```bash
# Submit batch job
curl -X POST http://localhost:8080/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "system_prompt": "Be concise.",
    "requests": [
      {"id": "1", "messages": [{"role": "user", "content": "Question 1"}]},
      {"id": "2", "messages": [{"role": "user", "content": "Question 2"}]},
      {"id": "3", "messages": [{"role": "user", "content": "Question 3"}]}
    ]
  }'
```

**Distribution logic:**
1. Jobs allocated to providers based on priority and available `workers` slots
2. Higher-priority providers get jobs first
3. Within same priority, jobs round-robin distributed based on capacity
4. Poll job status to read completed results while remaining requests continue

## Monitoring Multi-Provider Health

### Health Check Endpoints

```bash
# List all configured providers and their status
curl http://localhost:8080/providers

# Detailed health status including circuit breaker state
curl http://localhost:8080/health

# Real-time cost tracking across all providers
curl http://localhost:8080/stats
```

### Provider Status Response

```json
{
  "providers": [
    {
      "name": "openai",
      "enabled": true,
      "healthy": true,
      "workers": 3,
      "priority": 1,
      "circuit_breaker": "closed",
      "active_keys": 3
    },
    {
      "name": "openrouter",
      "enabled": true,
      "healthy": false,
      "workers": 2,
      "priority": 2,
      "circuit_breaker": "open",
      "cooldown_remaining": "2m30s"
    }
  ]
}
```

## Best Practices

### 1. Configure Priority for Clear Failover Chain

```json
"primary": { "priority": 1 },
"secondary": { "priority": 2 },
"backup": { "priority": 10 }  // Clear 3-level hierarchy
```

### 2. Match Workers to Rate Limits

```json
{
  "high-rate-limit": { "workers": 5 },   // Provider with 50 RPM limit
  "low-rate-limit": { "workers": 1 }     // Provider with 10 RPM limit
}
```

### 3. Use Multiple API Keys for Critical Providers

```json
{
  "critical-provider": {
    "api_keys": ["key1", "key2", "key3"],  // 3x capacity before failover
    "workers": 3,
    "priority": 1
  }
}
```

### 4. Test Failover Behavior

```bash
# 1. Start with all providers healthy
curl http://localhost:8080/health

# 2. Disable primary provider (simulate failure)
# 3. Verify traffic shifts to secondary
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"messages": [{"role": "user", "content": "Test"}]}'

# Check which provider handled the request via X-Llambo-Provider header
```

## Common Multi-Provider Issues

### Providers Not Failing Over

**Check:**
1. All providers have unique `priority` values
2. Circuit breaker cooldown periods are reasonable (60s for failures, 5m for rate limits)
3. Providers are actually `enabled: true`

### Uneven Load Distribution

**Cause:** Providers with same priority but different `workers` counts
**Fix:** Adjust `workers` to match desired capacity:

```json
{
  "fast-backend": { "priority": 1, "workers": 4 },  // 4x capacity
  "slow-backend": { "priority": 1, "workers": 1 }   // 1x capacity
}
```

### All Providers Circuit-Broken

**Recovery steps:**
1. Check `/health` endpoint for cooldown timers
2. Wait for cooldown periods to expire (auto-recovery)
3. Reduce `workers` counts to prevent immediate re-failure
4. Consider adding more API keys for critical providers

## Advanced: Provider-Specific Configuration

### API Paths

OpenAI-compatible serving normalizes `base_url` and appends the chat completion
path internally. Keep `base_url` at the provider root without `/v1`; `api_path`
is retained for older config files but is not read by the serving path.

### Provider-Specific Headers

```json
{
  "openrouter": {
    "extra_headers": {
      "HTTP-Referer": "https://myapp.com",
      "X-Title": "Production App"
    }
  },
  "gemini": {
    "extra_headers": {
      "X-Goog-Api-Key": "${GEMINI_API_KEY}"
    }
  }
}
```

### Thinking Tokens (z.ai)

```json
{
  "zai": {
    "extra_body": {
      "thinking": {
        "type": "enabled",
        "budget_tokens": 1024
      }
    }
  }
}
```

## Summary

Llambo's multi-provider system provides:

1. **Automatic failover** based on circuit breaker health
2. **Priority-based load balancing** with clear routing hierarchy
3. **Parallel processing** across all healthy backends
4. **API key rotation** for extended rate limits
5. **Cost optimization** through priority-based model selection

Start with 2-3 providers in a clear priority hierarchy, then expand based on your availability and cost requirements.
