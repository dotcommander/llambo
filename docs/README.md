# Llambo Documentation

```bash
llambo serve
llambo ping --models healthy -P nvidia
llambo prompt --models free "Reply with exactly OK"
```

Llambo is a **high-performance LLM gateway service** with parallel job processing, per-backend circuit breakers, model catalog health checks, and cross-backend load balancing.

Start with the quickstart, then open only the reference for the surface you are using.

## 📚 Documentation Sections

### 🚀 **[Getting Started](./getting-started/README.md)**
- Installation and quick start
- Basic configuration
- First API call examples
- Next steps for new users

**For:** New users who want to install and run Llambo quickly.

### 🔌 **[API Reference](./api/api.md)**
- Complete HTTP API documentation
- Endpoint specifications and examples
- Request/response formats
- Error handling and rate limits

**For:** Developers integrating Llambo into applications.

### 🛠️ **[Guides](./guides/README.md)**
- [CLI Guide](./guides/cli.md) for catalog, OpenRouter metadata, quality imports, ping, prompt, jobs, and routing commands
- [External Evaluation Scores](./guides/evals.md) for cache-only LLAMBO-7 category scores from frozen external benchmarks
- Practical examples for smart gateway, routing backtests, and parallel jobs
- Configuration guide (complete setup)
- Parallel job processing with pollable incremental results
- Circuit breaker and failover patterns
- Cost tracking and optimization

**For:** Users who need detailed configuration and advanced usage.

### 🏗️ **[Architecture](./architecture/architecture.md)**
- System architecture deep dive
- Component relationships and data flow
- Design patterns and implementation details
- Concurrency model and thread safety

**For:** Developers contributing to or extending Llambo.

## 📖 Quick Reference

### Core Features
- **Real-time parallel job processing** (not async batch API)
- **Per-backend circuit breakers** with automatic failover
- **Streaming results** as jobs complete
- **Cross-backend load balancing** per request
- **Multi-key rotation** for rate limit handling

### Key Commands
```bash
# Build and install
go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo

# Start server
llambo serve [--port 8080] [--host 127.0.0.1]

# Configuration
llambo config init      # Create default config
llambo config show      # Display current config

# Model catalog and live checks
llambo providers refresh nvidia
llambo models catalog --free
llambo providers refresh openrouter
llambo models catalog openrouter --metadata
llambo models catalog import-quality ./quality.json
llambo models discover-free -P nvidia
llambo ping --models healthy -P nvidia
llambo ping --models category:long_context -P openrouter
llambo evals
llambo evals --rank-by coding
llambo evals --rank-by writing
```

### Quick Start Example
```bash
# Start server
llambo serve

# Test health
curl http://localhost:8080/health

# First chat completion
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## 🗺️ Navigation Map

```
docs/
├── README.md (you are here)           # Central navigation hub
├── CONTRIBUTING.md                    # Development and contribution guide
├── CHANGELOG.md                       # Version history and changes
├── getting-started/
│   └── README.md                      # Installation and quick start
├── api/
│   ├── README.md                      # API overview
│   └── api.md                         # Complete API reference
├── guides/
│   ├── README.md                      # Guides overview
│   ├── cli.md                         # CLI commands and model catalog workflow
│   ├── evals.md                       # External-only LLAMBO-7 category scores
│   ├── practical-examples.md          # Hands-on examples for common workflows
│   └── configuration.md               # Configuration guide
└── architecture/
    ├── README.md                      # Architecture overview
    └── architecture.md                # Architecture deep dive
```

## 🔄 Documentation Updates

This documentation follows a **Laravel/Rust-style** organization pattern:

- **Central navigation hub** (this file) provides entry point
- **Section READMEs** give overviews and guide to deeper content
- **Detailed guides** provide comprehensive reference material
- **Cross-links** connect related concepts across sections
- **Changelog** tracks all notable changes between versions

## 🤝 Contributing

Found an issue or want to improve the documentation? See our comprehensive [Contributing Guide](./CONTRIBUTING.md) for:

- Development setup and build instructions
- Go conventions and project patterns
- Pull request process and commit message format
- Code quality standards and testing requirements

For documentation contributions:
- Edit markdown files directly
- Follow the existing structure and format
- Test changes locally before submitting
- Keep links updated when moving content

## 📞 Getting Help

If you encounter issues:
1. Check the relevant documentation section
2. Review the [Common Issues](./guides/configuration.md#common-issues) section
3. Verify your configuration with `llambo config show`
4. Test connectivity with `llambo ping --models healthy` or the health endpoint
