# Architecture overview

```bash
llambo serve
curl http://127.0.0.1:8080/health
```

## Boundaries

```text
CLI / HTTP client
       |
       v
cmd/ (Kong command and startup wiring)
       |
       v
internal/gateway/ (HTTP protocol and job lifecycle)
       |
       v
providers/ (selection, failover, keys, health, queues, accounting)
       |
       v
wormhole provider instances (backend protocol execution)
       |
       v
configured remote or loopback model endpoints
```

| Boundary | Owns | Does not own |
| --- | --- | --- |
| `cmd/` | CLI parsing, config selection, presentation, server startup | Provider protocol details |
| `internal/gateway/` | HTTP compatibility, validation, SSE, job state, auth and CORS middleware | Backend selection policy |
| `providers/` | Routing, retries, failover, key rotation, circuit breakers, concurrency and costs | Public HTTP envelopes |
| `internal/catalog/` | Model catalog, tags, health, quarantine and selection metadata | Live provider execution |
| `internal/evals/` | Sealed external evidence, deterministic scoring, projections and reports | Gateway routing |
| `wormhole` | Provider-specific request and response transport | Llambo policy or retry ownership |

## Construction

`llambo serve` validates the bind address, loads `config.json`, expands routing
providers, and calls `gateway.New`. The gateway constructs shared provider
orchestration, the parallel queue, embeddings support, and the job manager.
`Server.Start` registers method-aware routes and wraps them with optional bearer
authentication and exact-origin CORS handling.

Non-loopback binds require `--allow-remote`. Configure
`gateway.auth_token_env` before remote exposure. An empty
`gateway.allowed_origins` disables CORS.

## State and concurrency

- Provider configuration is immutable after server construction.
- Provider client maps and generation swaps are mutex protected.
- Each backend has its own worker limit, circuit-breaker state, and key state.
- The job manager bounds active jobs and requests per job.
- Routing metrics and event logs are persisted through provider-owned stores.
- Evaluation scoring reads frozen embedded references; refresh commands update
  source caches but do not redefine an installed percentile population.

## Shutdown

The serve command listens for interrupt or termination signals, gives the HTTP
server a bounded graceful-shutdown window, cancels job cleanup, and closes
provider resources once.
