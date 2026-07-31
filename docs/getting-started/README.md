# Getting Started

## What is Llambo?

Llambo (LLM + Lamborghini) is a **high-performance LLM gateway service** designed to simplify and enhance your interactions with multiple large language model providers. Instead of managing separate API integrations for OpenAI, Anthropic, Google, and other providers, Llambo provides a unified gateway with intelligent routing, automatic failover, and parallel processing capabilities.

The service acts as a single entry point that handles provider abstraction, circuit breaker patterns for reliability, and real-time parallel job execution. Whether you're building applications that need high availability, handling rate limits across multiple API keys, or processing batches of requests efficiently, Llambo manages the complexity so you can focus on your application logic.

## Key Features

- **Parallel Job Processing**: Execute multiple requests concurrently across healthy backends with real-time streaming results
- **Circuit Breakers & Automatic Failover**: Per-backend health tracking with automatic failover to maintain service availability
- **Multi-Provider Support**: Unified interface for OpenAI, Anthropic, Google, OpenRouter, and other LLM providers
- **Key Rotation**: Intelligent rotation across multiple API keys to handle rate limits effectively
- **Configuration-Driven**: Add new providers by editing a JSON config file - no code changes required
- **Cost Tracking**: Monitor token usage and costs across all providers in real-time

## Who Should Use Llambo?

- **Developers** building applications that require high availability LLM access
- **Teams** managing multiple API keys and providers across different environments
- **Applications** needing parallel processing of batch requests with streaming results
- **Systems** requiring automatic failover and circuit breaker protection against provider outages
- **Organizations** wanting centralized cost tracking and usage monitoring across LLM providers

## Installation

```bash
# Build and install
go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo
```

## Quick Start

```bash
# Start the server
llambo serve

# Check health
curl http://localhost:8080/health

# Check configured catalog models
llambo ping --models healthy

# Test a chat completion
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Next Steps

- [CLI Guide](../guides/cli.md)
- [Configuration Guide](../guides/configuration.md)
- [API Reference](../api/api.md)
- [Architecture](../architecture/architecture.md)
