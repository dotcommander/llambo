# Error Handling

Llambo provides consistent error responses across all API endpoints. This guide covers HTTP status codes, error response format, and circuit breaker behavior.

## HTTP Status Codes

Llambo uses standard HTTP status codes with specific meanings:

### Client Errors (4xx)

| Code | Name | Description | When Returned |
|------|------|-------------|---------------|
| `400` | Bad Request | Invalid request format or missing required fields | Invalid JSON, missing required parameters, malformed requests |
| `401` | Unauthorized | Authentication failed | Invalid or missing API key in provider configuration |
| `403` | Forbidden | Insufficient permissions | Valid API key but insufficient permissions for requested resource |
| `404` | Not Found | Resource not found | Requested job ID doesn't exist, invalid endpoint |
| `429` | Too Many Requests | Rate limit exceeded | Provider rate limit reached, circuit breaker activated |
| `451` | Unavailable For Legal Reasons | Provider blocked by policy | Legal/regional restrictions on provider |

### Server Errors (5xx)

| Code | Name | Description | When Returned |
|------|------|-------------|---------------|
| `500` | Internal Server Error | Unexpected server error | Unhandled exceptions, programming errors |
| `502` | Bad Gateway | Upstream provider error | Provider returned 5xx error, connection issues |
| `503` | Service Unavailable | Service overloaded or degraded | No healthy providers available, circuit breakers active |
| `504` | Gateway Timeout | Upstream timeout | Provider request timeout exceeded |

## Error Response Format

All error responses follow the same JSON structure:

```json
{
  "error": {
    "message": "Human-readable error description",
    "type": "error_type",
    "code": "optional_error_code"
  }
}
```

### Error Types

| Type | Description | Example |
|------|-------------|---------|
| `invalid_request` | Request validation failed | Missing required field, invalid JSON |
| `upstream_error` | Provider returned error | Provider API error, timeout |
| `not_found` | Resource doesn't exist | Job ID not found |
| `rate_limited` | Rate limit exceeded | Too many requests to provider |
| `not_implemented` | Feature not available | Streaming not supported |
| `not_configured` | Missing configuration | Embedding provider not configured |

### Example Error Responses

**400 Bad Request:**
```json
{
  "error": {
    "message": "Messages array is required",
    "type": "invalid_request"
  }
}
```

**404 Not Found:**
```json
{
  "error": {
    "message": "Job not found",
    "type": "not_found"
  }
}
```

**502 Bad Gateway:**
```json
{
  "error": {
    "message": "Provider error: 429 Too Many Requests",
    "type": "upstream_error"
  }
}
```

## Rate Limiting (429) Handling

Llambo implements sophisticated rate limiting and circuit breaker logic to handle provider limitations.

### Key Rotation on 429

When a provider returns HTTP 429:

1. **Key rotation** (if multiple API keys configured):
   - Current key marked as rate-limited
   - Rotate to next available key
   - Retry request with new key
   - Continue until all keys exhausted

2. **Circuit breaker activation**:
   - Provider marked as unhealthy
   - Requests fail over to next available provider
   - 5-minute cooldown before retrying provider

### Multi-Key Configuration

Configure multiple API keys in `~/.config/llambo/config.json`:

```json
{
  "providers": {
    "openai": {
      "api_keys": ["sk-key1", "sk-key2", "sk-key3"],
      "workers": 3
    }
  }
}
```

With 3 API keys, you get 3x the rate limit before failover.

### Retry-After Support

Llambo respects `Retry-After` headers from providers:
- **Seconds format**: `Retry-After: 30`
- **Date format**: `Retry-After: Wed, 21 Oct 2025 07:28:00 GMT`

When a `Retry-After` header is present, the cooldown period is adjusted accordingly.

## Circuit Breaker Behavior

The circuit breaker protects against unhealthy providers and implements intelligent failover.

### Failure Detection

| Failure Pattern | Action | Cooldown |
|-----------------|--------|----------|
| HTTP 429 (no keys left) | Immediate disable | 5 minutes |
| "rate limit" in error message | Immediate disable | 5 minutes |
| "quota" in error message | Immediate disable | 5 minutes |
| 3+ consecutive failures | Disable | 60 seconds |
| Success after cooldown | Re-enable, reset counter | - |

### Auto-Recovery

Circuit breakers auto-recover after cooldown:
- **Rate limit errors**: 5 minutes (`RateLimitCooldown`)
- **General failures**: 60 seconds (`FailureCooldown`)
- **Quota errors**: Use rate limit cooldown if failures ≥ 10

### Health Monitoring

Check circuit breaker status via `/health` endpoint:

```json
{
  "status": "degraded",
  "backends": {
    "openai": {
      "healthy": false,
      "failures": 3,
      "disabled_at": "2025-01-17T23:41:00Z"
    }
  }
}
```

## Error Classification

Llambo classifies errors for appropriate handling:

### Classification Categories

| Category | HTTP Codes | String Patterns | Handling |
|----------|------------|-----------------|----------|
| **Rate Limit** | 429 | "rate limit", "too many requests", "throttl" | Circuit breaker, key rotation |
| **Quota** | - | "quota", "exceeded your current quota", "billing" | Circuit breaker (5 min) |
| **Auth** | 401, 403 | "invalid api key", "authentication", "unauthorized" | Circuit breaker, config fix required |
| **Config** | - | "invalid model", "unsupported value", "does not exist" | Circuit breaker, code/config fix |
| **Transient** | 502, 503, 504 | "connection reset", "timeout", "temporary failure" | Retry with backoff |
| **Unknown** | Other codes | No match | Generic error response |

### Error Pattern Matching

Errors are classified using:
1. **HTTP status code** (primary method)
2. **String pattern matching** (fallback when no status code)
3. **Embedded status codes** in error messages

## Best Practices

### Client-Side Handling

1. **Check error types**:
   ```javascript
   if (error.type === 'rate_limited') {
     // Implement exponential backoff
   }
   ```

2. **Monitor health endpoint**:
   ```bash
   curl http://localhost:8080/health
   ```

3. **Use job queue for batch processing**:
   - Submit jobs via `/v1/jobs`
   - Results stream as they complete
   - Handles provider failures automatically

### Configuration

1. **Enable multiple providers** for failover:
   ```json
   {
     "providers": {
     "openai": { "priority": 1 },
     "openrouter": { "priority": 2 },
     "anthropic": { "priority": 3 }
     }
   }
   ```

2. **Configure multiple API keys** per provider:
   ```json
   {
     "api_keys": ["sk-key1", "sk-key2", "sk-key3"],
     "workers": 3
   }
   ```

3. **Monitor statistics** via `/stats` endpoint:
   ```bash
   curl http://localhost:8080/stats
   ```

## Troubleshooting

### Common Issues

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| All requests failing | All providers circuit-broken | Check `/health`, wait for cooldown |
| 502 errors | Provider API issues | Check provider status, verify API keys |
| 401 errors | Invalid API key | Update config.json with valid key |
| Slow responses | Rate limiting active | Add more API keys, reduce request rate |

### Debug Tools

1. **Health check**:
   ```bash
   curl http://localhost:8080/health
   ```

2. **Provider list**:
   ```bash
   curl http://localhost:8080/providers
   ```

3. **Statistics**:
   ```bash
   curl http://localhost:8080/stats
   ```

4. **Verbose logging** (start server with `-v` flag):
   ```bash
   llambo serve -v
   ```