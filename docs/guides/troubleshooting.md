# Troubleshooting Guide

This guide helps diagnose and resolve common issues with Llambo.

## Quick Diagnostic Commands

Use these commands to check system health:

```bash
# Check provider status
curl http://localhost:8080/health

# List configured providers
curl http://localhost:8080/providers

# Show configuration
llambo config show

# Check catalog-selected models without starting the server
llambo ping --models healthy

# Test a simple request
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 10
  }'
```

## Common Issues

### HTTP 404 from OpenAI-compatible Provider

| Aspect | Description |
|--------|-------------|
| **Symptom** | Requests fail with HTTP 404 errors from OpenAI-compatible providers |
| **Cause** | Double `/v1` in URL. The OpenAI SDK appends `/v1/chat/completions` to the BaseURL, so including `/v1` in config creates a double `/v1/v1/chat/completions` path |
| **Solution** | Remove `/v1` from `base_url` in config.json |

**Example Fix:**
```json
// Wrong
"base_url": "https://api.openai.com/v1"

// Correct
"base_url": "https://api.openai.com"
```

### Provider Uses Wrong BaseURL

| Aspect | Description |
|--------|-------------|
| **Symptom** | Requests route to incorrect API endpoint or wrong provider |
| **Cause** | Each backend needs its own OpenAI client with correct BaseURL configuration |
| **Solution** | This is handled automatically by Llambo - each backend gets a dedicated client. Verify your configuration with `llambo config show` |

**Verification Steps:**
1. Run `llambo config show` to see configured BaseURLs
2. Check `/health` endpoint to see which providers are active
3. Ensure `enabled: true` for each provider you want to use

### Changes Don't Take Effect After Build

| Aspect | Description |
|--------|-------------|
| **Symptom** | Code changes compile but don't appear when running `llambo` |
| **Cause** | Binary built to `./llambo` but user runs `~/go/bin/llambo` |
| **Solution** | Always symlink after building: `ln -sf $(pwd)/llambo ~/go/bin/llambo` |

**Correct Build Process:**
```bash
# Build AND symlink to PATH
go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo

# Or just run without building
go run . <command>
```

## Interpreting `/health` Output

The `/health` endpoint provides real-time diagnostics about provider status and system health.

### Sample `/health` Response
```json
{
  "status": "healthy",
  "timestamp": "2026-01-18T13:22:45Z",
  "providers": [
    {
      "name": "openai",
      "enabled": true,
      "healthy": true,
      "circuit_breaker": "closed",
      "consecutive_failures": 0,
      "last_error": null,
      "available_keys": 3,
      "total_keys": 3
    },
    {
      "name": "openrouter",
      "enabled": true,
      "healthy": false,
      "circuit_breaker": "open",
      "consecutive_failures": 5,
      "last_error": "rate limit exceeded",
      "available_keys": 0,
      "total_keys": 2,
      "cooldown_until": "2026-01-18T13:27:45Z"
    }
  ]
}
```

### Key Diagnostic Fields

| Field | Healthy State | Problem Indicators |
|-------|--------------|-------------------|
| `status` | `"healthy"` | `"degraded"` (some providers down) or `"unhealthy"` (all providers down) |
| `providers[].healthy` | `true` | `false` indicates provider cannot process requests |
| `providers[].circuit_breaker` | `"closed"` | `"open"` means provider temporarily disabled due to failures |
| `providers[].consecutive_failures` | `0` | `>=3` triggers circuit breaker (60s cooldown) |
| `providers[].available_keys` | `>0` | `0` means all API keys exhausted (5min cooldown) |
| `providers[].last_error` | `null` | Error message shows last failure reason |
| `providers[].cooldown_until` | `null` | Timestamp when provider will auto-recover |

### Diagnosis Scenarios

**Scenario 1: All Providers Circuit-Broken**
- **Symptom**: All requests fail with 502 errors
- **/health shows**: All providers have `circuit_breaker: "open"`
- **Solution**: Wait for cooldown periods to expire, check API keys, reduce request rate

**Scenario 2: Single Provider Failing**
- **Symptom**: Some requests work, others fail
- **/health shows**: One provider has `healthy: false` but others are healthy
- **Solution**: Llambo will automatically failover to healthy providers

**Scenario 3: Rate Limit Issues**
- **Symptom**: Intermittent 429 errors
- **/health shows**: `available_keys: 0` and `last_error: "rate limit"`
- **Solution**: Add more API keys to config, reduce `workers` count, wait for key cooldown

## Advanced Diagnostics

### Checking Token Usage and Costs
```bash
curl http://localhost:8080/stats
```

### Provider-Specific Testing
```bash
# Test OpenAI provider directly (bypassing gateway)
curl https://api.openai.com/v1/models \
  -H "Authorization: Bearer $OPENAI_API_KEY"

# Test OpenRouter provider directly
curl https://openrouter.ai/api/v1/models \
  -H "Authorization: Bearer $OPENROUTER_API_KEY" \
  -H "HTTP-Referer: http://localhost:8080"
```

### Routing Diagnostics
Record routing metrics and replay route events:

```bash
llambo ping --models healthy --record-routing-metrics
llambo route simulate --modes fastest,cheapest,balanced,quality
```

## Prevention Best Practices

1. **Always symlink after building**: `ln -sf $(pwd)/llambo ~/go/bin/llambo`
2. **Never include `/v1` in base_url**: Let the SDK handle path construction
3. **Use multiple API keys**: Configure 3+ keys per provider for automatic rotation
4. **Monitor `/health` regularly**: Set up health checks in production
5. **Check provider status pages**: Some providers publish API status dashboards

## Still Stuck?

If issues persist:
1. Verify API keys are valid and have sufficient quota
2. Check network connectivity to provider APIs
3. Review server logs for detailed error messages
4. Test with a minimal configuration
5. Rebuild the PATH-visible binary: `go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo`
