# Architecture

```bash
go test ./internal/gateway ./providers ./cmd
```

Llambo separates command orchestration, HTTP protocol handling, provider
execution, and persisted catalog/evaluation data. Start with the overview, then
follow the request path you need.

| Document | Use it for |
| --- | --- |
| [Architecture overview](architecture.md) | Package ownership and boundaries |
| [Request flow](request-flow.md) | Chat, streaming, embeddings, and job lifecycles |
| [Provider system](providers.md) | Routing, wormhole clients, retries, keys, and circuit breakers |
| [Module map](module-docs.md) | File-level navigation |

Related operational references:

- [Circuit breakers](../guides/circuit-breakers.md)
- [Key rotation](../guides/key-rotation.md)
- [Jobs API](../api/jobs.md)
- [Configuration](../guides/configuration.md)

## Current package map

```text
cmd/                 Kong commands, selectors, reporting, server startup
internal/catalog/    persisted model metadata, health, tags, selection
internal/costs/      price-file and models.dev cost data
internal/evals/      sealed evaluation sources, scoring, projections, reports
internal/gateway/    HTTP protocols, streaming, jobs, status endpoints
internal/modelsdev/  models.dev fetch and configuration synchronization
providers/           routing, queues, wormhole clients, retries, telemetry
```

The gateway owns policy and lifecycle. Each configured backend owns a dedicated
wormhole provider instance; wormhole performs leaf protocol execution while
Llambo owns routing, failover, key rotation, queues, circuit breakers, and cost
tracking.
