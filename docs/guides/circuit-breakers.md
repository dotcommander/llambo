# Circuit Breakers Guide

Comprehensive guide to Llambo's circuit breaker system for fault tolerance and failover.

## Overview

Llambo implements **per-backend circuit breakers** that automatically detect unhealthy providers and route traffic to healthy alternatives. This prevents cascading failures and provides automatic failover.

## Three Failure States

### 1. **Closed** (Healthy)
- **State**: Backend is fully operational
- **Behavior**: Receives all traffic according to priority/load balancing
- **Transition to Open**: On rate limit/quota error or 3+ consecutive failures

### 2. **Open** (Unhealthy)
- **State**: Backend is temporarily disabled
- **Behavior**: No requests routed to this backend
- **Recovery**: Auto-recovery after cooldown period expires
- **Transition to Half-Open**: After cooldown expires, backend becomes eligible for testing

### 3. **Half-Open** (Testing)
- **State**: Backend is recovering, eligible for requests after cooldown expires
- **Behavior**: First successful request immediately re-enables backend; failure triggers disable with new cooldown
- **Recovery**: Success transitions to Closed; failure transitions to Open with new cooldown
- **Implementation**: No formal state tracking - behavior matches half-open pattern through auto-recovery logic

## Triggers (Exact Conditions)

Circuit breakers trigger based on these exact conditions:

| Trigger | Action | Cooldown |
|---------|--------|----------|
| **HTTP 429** (no keys left) | Immediate disable | 5 minutes (`RateLimitCooldown`) |
| **"rate limit" in error** | Immediate disable | 5 minutes (`RateLimitCooldown`) |
| **"quota" in error** | Immediate disable | 5 minutes (`RateLimitCooldown`) |
| **3+ consecutive failures** (`MaxConsecutiveFailures`) | Disable | 60 seconds (`FailureCooldown`) |
| **Success after cooldown** | Re-enable, reset counter | - |
| **Manual success via `RecordSuccess`** | Immediate re-enable | - |

### Detection Details

**Rate limit/quota errors** are detected by:
- HTTP status code 429
- Error messages containing: `rate limit`, `rate_limit`, `quota`, `too many requests`
- Provider protocol errors and raw HTTP errors are classified

**Consecutive failures** count includes:
- Any error from the backend (except those that trigger immediate disable)
- Failures reset to 0 on successful request

## Recovery & Cooldown Periods

### Auto-Recovery Behavior

Circuit breakers **automatically recover** after cooldown periods:

| Condition | Cooldown | Auto-Recovery |
|-----------|----------|---------------|
| Rate limit/quota error | 5 minutes (`RateLimitCooldown`) | Yes, after 5 minutes |
| 3+ consecutive failures | 60 seconds (`FailureCooldown`) | Yes, after 60 seconds |
| ≥10 failures (`QuotaErrorThreshold`) | 5 minutes (`RateLimitCooldown`) | Yes, after 5 minutes |

### Cooldown Logic

```go
if failures >= QuotaErrorThreshold (10) {
    cooldown = RateLimitCooldown (5 minutes)
} else {
    cooldown = FailureCooldown (60 seconds)
}
```

**Why**: High failure counts (≥10) likely indicate quota exhaustion, not transient issues.

### Success-Based Recovery

A successful request **immediately** recovers a backend:
- Resets failure count to 0
- Sets `Disabled = false`
- Returns backend to Closed state

## Key Rotation Integration

Circuit breakers integrate with **multi-key rotation**:

| Scenario | Action |
|----------|--------|
| HTTP 429 with remaining keys | Rotate key, continue using same provider |
| HTTP 429 with all keys exhausted | Circuit-break provider |
| Key cooldown expires | Key becomes available for rotation |

**Benefit**: With 3 API keys per provider, you get 3× the rate limit before failover.

## Health Monitoring

### Health Endpoints

Check circuit breaker status via API:

```bash
# List all providers with health status
curl http://localhost:8080/providers

# Health check with backend status
curl http://localhost:8080/health
```

### Health States

Each backend reports:
- `healthy`: Accepting requests
- `disabled`: Circuit breaker open
- `failures`: Consecutive failure count
- `last_failure`: Timestamp of last failure
- `disabled_at`: When circuit breaker opened

## Example Scenarios

### Scenario 1: Rate Limit Hit
1. OpenAI returns HTTP 429
2. Circuit breaker immediately disables OpenAI backend (5 min cooldown)
3. Requests routed to OpenRouter (priority 2)
4. After 5 minutes, OpenAI auto-recovers

### Scenario 2: Intermittent Failures
1. OpenAI fails 2 times (network issues)
2. Third failure triggers circuit breaker (60 sec cooldown)
3. OpenRouter handles requests
4. After 60 seconds, OpenAI auto-recovers

### Scenario 3: Quota Exhausted
1. OpenAI returns "quota exceeded" error
2. Circuit breaker immediately disables (5 min cooldown)
3. 10+ accumulated failures use 5 min cooldown (not 60 sec)
4. After 5 minutes, backend recovers

## Configuration Impact

Circuit breakers work with provider configuration:

| Config Field | Impact on Circuit Breaker |
|--------------|---------------------------|
| `workers` | Affects capacity before failover |
| `priority` | Determines failover order |
| `enabled` | Must be `true` to participate |
| `api_keys` | Multi-key rotation extends rate limits |

## Best Practices

### For High Availability
- Configure multiple providers with different `priority` values
- Use 3+ API keys per provider for key rotation
- Set appropriate `workers` based on rate limits

### For Cost Optimization
- Use cheaper providers with higher priority
- Set premium providers as low-priority fallbacks
- Monitor `failures` to detect problematic backends

### Monitoring
- Watch `/health` endpoint for disabled backends
- Check `failures` count to identify trends
- Use key rotation logs to track 429 responses

## Common Questions

**Q: Can I manually disable a backend?**
A: Yes, set `enabled: false` in config or restart with modified config.

**Q: How are "consecutive failures" counted?**
A: Any error from the backend increments the counter. Success resets to 0.

**Q: What if all backends are circuit-broken?**
A: Requests fail with 503 Service Unavailable. Monitor health to prevent this.

**Q: Can I change cooldown times?**
A: Modify constants in `providers/constants.go`: `RateLimitCooldown`, `FailureCooldown`.

**Q: How does this work with batch jobs?**
A: Circuit breakers apply per-backend. Jobs distribute across healthy backends only.
