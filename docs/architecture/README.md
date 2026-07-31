# Architecture Documentation

Deep dive into Llambo's internal architecture and design patterns.

## Available Documentation

- [Architecture Overview](architecture.md) - Complete architectural deep dive
- [Request Flow](request-flow.md) - HTTP server and request handling
- [Provider System](providers.md) - Backend abstraction and client management
- [Circuit Breaker Pattern](../guides/circuit-breakers.md) - Health tracking and failover implementation
- [Job Processing](../api/jobs.md) - Parallel job execution with streaming results
- [Key Rotation](../guides/key-rotation.md) - Multi-key management for rate limits

## System Components

```
cmd/
  serve.go             # Gateway server command
  config.go            # Config management
  root.go              # Root command

internal/
  gateway/
    server.go          # HTTP server setup
    handlers.go        # Request handlers
    jobs.go            # Parallel job processing
    types.go           # OpenAI-compatible types

providers/
  openai_provider.go   # OpenAI-compatible provider with failover
  client_factory.go    # Per-backend client management
  key_rotator.go       # Multi-key rotation on 429
  queue.go             # BackendQueue (parallel job processing)
  circuit_breaker.go   # Per-backend health tracking
  cost_tracker.go      # Token usage and cost aggregation
  embeddings.go        # Embedding provider
  config.go            # Config loading
```

## Design Patterns

- **Gateway Pattern**: Single entry point for multiple LLM providers
- **Circuit Breaker**: Per-backend health tracking with automatic failover
- **Key Rotation**: Multi-key management to handle rate limits
- **Parallel Processing**: Concurrent job execution across providers
- **Streaming Results**: Real-time results as jobs complete

## Configuration-Driven Architecture

All provider configuration comes from `~/.config/llambo/config.json`:

```json
{
  "providers": {
    "openai": {
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "api_keys": ["sk-key1", "sk-key2", "sk-key3"],
      "max_tokens": 4096,
      "workers": 3,
      "priority": 1,
      "enabled": true
    }
  }
}
```

**Adding a new provider = just edit config.json.** No code changes needed.
