# Operational Endpoints

Endpoints for monitoring Llambo in production.

---

## Health Check

Check gateway health and backend circuit breaker states.

### Request

```http
GET /health
```

### Response

```json
{
  "status": "healthy",
  "uptime_seconds": 3600,
  "backends": {
    "openai": {
      "healthy": true,
      "failures": 0
    },
    "openrouter": {
      "healthy": false,
      "failures": 3,
      "disabled_at": "2026-01-17T12:05:00Z"
    }
  }
}
```

#### Gateway Status

| Status | Description |
|--------|-------------|
| `healthy` | All backends are healthy |
| `degraded` | One or more backends are unhealthy |

#### Backend Health Fields

| Field | Type | Description |
|-------|------|-------------|
| `healthy` | boolean | Whether backend is currently accepting requests |
| `failures` | integer | Consecutive failure count |
| `disabled_at` | string (ISO 8601) | When backend was disabled by circuit breaker (only present if disabled) |

### Circuit Breaker Behavior

Backends are disabled by the circuit breaker when:

1. **Rate limit errors (HTTP 429)** - Immediate 5-minute cooldown
2. **Quota errors** - Immediate 5-minute cooldown
3. **3+ consecutive failures** - 60-second cooldown

Disabled backends auto-recover after their cooldown period expires.

---

## Providers

List configured providers with their current operational state.

### Request

```http
GET /providers
```

### Response

```json
{
  "providers": [
    {
      "name": "openai",
      "model": "gpt-4o",
      "enabled": true,
      "healthy": true,
      "priority": 1,
      "workers": 3
    },
    {
      "name": "openrouter",
      "model": "anthropic/claude-3-5-sonnet",
      "enabled": true,
      "healthy": false,
      "priority": 2,
      "workers": 2
    }
  ]
}
```

#### Provider Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Provider identifier (matches config.json) |
| `model` | string | Configured model for this provider |
| `enabled` | boolean | Whether provider is enabled in config |
| `healthy` | boolean | Current circuit breaker health status |
| `priority` | integer | Load balancing priority (lower = higher priority) |
| `workers` | integer | Parallel request slots available |

**Note:** A provider may be `enabled: true` but `healthy: false` if temporarily disabled by circuit breaker.

---

## Stats

Get token usage and cost statistics aggregated across all providers.

### Request

```http
GET /stats
```

### Response

```json
{
  "providers": {
    "openai": {
      "requests": 900,
      "prompt_tokens": 50000,
      "completion_tokens": 10000,
      "total_tokens": 60000,
      "total_cost_usd": 0.1234
    },
    "openrouter": {
      "requests": 334,
      "prompt_tokens": 6789,
      "completion_tokens": 2345,
      "total_tokens": 9134,
      "total_cost_usd": 0.089
    }
  },
  "total": {
    "requests": 1234,
    "prompt_tokens": 56789,
    "completion_tokens": 12345,
    "total_tokens": 69134,
    "total_cost_usd": 0.2124
  }
}
```

#### Statistics Fields

| Field | Type | Description |
|-------|------|-------------|
| `requests` | integer | Total number of successful requests |
| `prompt_tokens` | integer | Total prompt tokens consumed |
| `completion_tokens` | integer | Total completion tokens generated |
| `total_tokens` | integer | Total tokens (prompt + completion) |
| `total_cost_usd` | float | Estimated cost in USD |

**Per-provider stats:** Available under the `providers` object keyed by provider name.

**Aggregate stats:** Available in the `total` object summarizing across all providers.

### Cost Calculation

Costs are estimated using provider-specific token pricing:
- OpenAI: $0.01/1K prompt tokens, $0.03/1K completion tokens (gpt-4o)
- OpenRouter: Uses provider's published pricing
- Other providers: Configured pricing or estimates

---

## Usage Examples

### Monitoring Health

```bash
# Check gateway health
curl http://localhost:8080/health

# Parse with jq
curl -s http://localhost:8080/health | jq '.status'
curl -s http://localhost:8080/health | jq '.backends | map_values(.healthy)'
```

### Provider Status Dashboard

```bash
# List all providers
curl -s http://localhost:8080/providers | jq '.providers[] | "\(.name): \(.healthy) (enabled=\(.enabled))"'
```

### Cost Tracking

```bash
# Get total cost
curl -s http://localhost:8080/stats | jq '.total.total_cost_usd'

# Get per-provider breakdown
curl -s http://localhost:8080/stats | jq '.providers | to_entries[] | "\(.key): \(.value.total_cost_usd) USD"'
```

### Integration with Monitoring Tools

These endpoints are designed for integration with:
- **Prometheus/Grafana** - Poll `/stats` for metrics
- **Health checks** - Use `/health` for readiness/liveness probes
- **Alerting** - Monitor `status: "degraded"` or unhealthy backends
- **Cost dashboards** - Track `total_cost_usd` over time