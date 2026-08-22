# Module map

```bash
rg --files cmd internal providers | sort
```

Use this map to find the current owner before changing behavior.

| Area | Primary files | Responsibility |
| --- | --- | --- |
| CLI root | `cmd/root.go`, `cmd/root_commands_*.go` | Kong command tree and shared flags |
| Server startup | `cmd/serve.go` | Bind safety, config loading, lifecycle |
| Model CLI | `cmd/models*.go`, `cmd/model_selector.go` | Inventory, catalog, tags, selectors |
| Runtime probes | `cmd/ping*.go`, `cmd/prompt*.go` | Health checks and prompt fanout |
| Routing CLI | `cmd/route*.go` | Queries, canaries, preferences, reports |
| Evaluation CLI | `cmd/evals*.go` | External evidence reports and local-eval orchestration |
| Gateway server | `internal/gateway/server.go` | Route registration, middleware, shutdown |
| OpenAI chat | `internal/gateway/handlers.go`, `openai_stream.go` | Chat decoding, JSON and SSE responses |
| Anthropic | `internal/gateway/anthropic*.go` | Messages translation and streaming |
| Jobs | `internal/gateway/job*.go`, `job_handlers.go`, `job_manager.go` | Job state, admission, cancellation |
| Catalog | `internal/catalog/*.go` | Persisted model metadata and selection |
| Evaluations | `internal/evals/*.go` | Source fetch, frozen scoring, projections, reports |
| Provider construction | `providers/client_factory.go`, `client_provider.go` | Dedicated wormhole instances |
| Chat execution | `providers/chat_execution*.go`, `chat_request*.go` | Attempts, retries, outcomes, key changes |
| Routing | `providers/intelligent_router.go`, `routing_*.go` | Policy, estimates, metrics, events |
| Resilience | `providers/circuit_breaker.go`, `key_rotator.go`, `queue.go` | Health, credentials, concurrency |
| Costs | `providers/cost_tracker.go`, `internal/costs/*.go` | Usage accounting and price data |

For behavior diagrams, see [Request flow](request-flow.md). For provider
configuration and failure policy, see [Provider system](providers.md).
