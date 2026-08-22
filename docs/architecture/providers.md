# Provider system

```bash
llambo providers
llambo ping --models healthy
```

Provider configuration lives in `~/.config/llambo/config.json`. Adding a normal
OpenAI-compatible backend is configuration work; it does not require a new
gateway handler.

## Execution ownership

Llambo creates one wormhole provider instance per enabled backend. This keeps
base URLs, credentials, headers, body overrides, and model selection isolated.
Wormhole retries are disabled for these instances because Llambo owns retry,
failover, key rotation, and circuit-breaker transitions.

```text
request
  -> routing eligibility and policy
  -> selected healthy backend
  -> backend concurrency slot
  -> dedicated wormhole provider
  -> success accounting or classified failure
  -> key rotation / retry / failover when eligible
```

## Minimal configuration

```json
{
  "default_provider": "openai",
  "providers": {
    "openai": {
      "base_url": "https://api.openai.com",
      "model": "gpt-5.6-sol",
      "env_var": "OPENAI_API_KEY",
      "enabled": true
    }
  }
}
```

Keep OpenAI-compatible `base_url` values at the provider root without `/v1`.
The serving path ignores legacy `api_path`. Prefer `env_var` or `api_keys` over
persisting a credential in the config file.

## Selection and failure handling

- Lower `priority` values are preferred by priority-based selection.
- Routing modes can consider capability fit, observed latency, known price,
  reliability, allow/deny lists, and configured budgets.
- A provider with no worker capacity waits in its bounded queue.
- A `429` rotates to another configured key when possible.
- Exhausted keys, quota failures, and repeated failures open that backend's
  circuit breaker; other healthy backends remain eligible.
- Successful recovery closes the circuit and resets consecutive failures.

## Provider configuration fields

| Field | Purpose |
| --- | --- |
| `base_url` | Provider root endpoint |
| `model` / `models` | Default model and optional selectable variants |
| `provider_type` | Protocol family; defaults to OpenAI-compatible |
| `api_keys` / `env_var` | Credential sources |
| `workers` | Concurrent request slots; defaults to 2 |
| `priority` | Selection priority; lower is earlier |
| `max_tokens` / `temperature` | Provider defaults |
| `extra_headers` | Provider-specific request headers |
| `extra_body` | Provider request body additions |
| `extra_body_by_model` | Per-model body overrides |
| `requires_key` | Disable only for trusted keyless/local endpoints |
| `capabilities` | Optional routing tags |

See [Configuration](../guides/configuration.md) for the complete schema and
[Request flow](request-flow.md) for lifecycle details.
