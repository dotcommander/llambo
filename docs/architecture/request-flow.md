# Request flow

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"Reply with OK"}]}'
```

## HTTP admission

`Server.Start` registers method-specific routes. Requests then pass through:

1. Exact-origin CORS handling when `gateway.allowed_origins` is configured.
2. Bearer authentication when `gateway.auth_token_env` or `auth_token` is set.
3. Endpoint-specific decoding and validation.
4. Provider or job execution.
5. OpenAI-compatible, Anthropic-compatible, or operational response encoding.

## Chat completion

```text
POST /v1/chat/completions
  -> decode messages and options
  -> choose a healthy eligible backend
  -> acquire its worker slot
  -> execute through its wormhole provider
  -> classify outcome and update health/cost/routing telemetry
  -> retry, rotate a key, or fail over when eligible
  -> return JSON plus X-Llambo-Provider and X-Llambo-Model
```

With `stream: true`, the handler writes Server-Sent Events as provider content
deltas arrive and emits the finish reason when the stream completes.

## Anthropic Messages

`POST /v1/messages` translates the Anthropic-compatible request into Llambo's
internal chat flow, then translates the final response or stream events back to
the Messages format. `LLAMBO_ANTHROPIC_STRICT` enables stricter compatibility
checks at process startup.

## Embeddings

`POST /v1/embeddings` accepts a string or string array. Provider construction
prefers an OpenAI/OpenAI-type backend, then falls back to another enabled
backend. The gateway delegates execution and returns the OpenAI-compatible
embedding envelope.

## Parallel jobs

```text
POST /v1/jobs              -> validate limits, create job, return HTTP 202
GET /v1/jobs/{id}          -> return current status and completed results
POST /v1/jobs/{id}/cancel  -> cancel non-terminal work and return job state
```

The HTTP contract is polling. Internally, the queue schedules requests across
healthy backends and records each result as it completes. `max_active_jobs` and
`max_requests_per_job` bound admission.

## Failure ownership

| Failure | Owner |
| --- | --- |
| Invalid JSON, method, auth, origin, or job ID | Gateway handler/middleware |
| No eligible backend or routing budget exhausted | Provider router |
| Rate limit or quota | Key rotation and circuit breaker |
| Provider transport/protocol error | Wormhole result, classified by Llambo |
| Client cancellation or timeout | Request context |

See [API errors](../api/errors.md) for response envelopes and
[Provider system](providers.md) for retry and health policy.
