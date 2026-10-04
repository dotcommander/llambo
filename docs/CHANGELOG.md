# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-10-04

### Added
- OpenAI-compatible tool calling end to end: `tools`, `tool_choice`, `tool_calls`, and `stop` on `/v1/chat/completions` and batch job requests, including streaming tool-call deltas and `finish_reason: "tool_calls"`
- Anthropic Messages translation of assistant `tool_use` and user `tool_result` content blocks into the OpenAI wire format
- `response_format` passthrough and per-request sampling parameters (`temperature`, `top_p`, `max_tokens`) for batch job requests
- `stream_options.include_usage` for streaming chat completions
- Configurable gateway timeouts: `gateway.handler_timeout_seconds` (90), `gateway.write_timeout_seconds` (120), and `gateway.shutdown_timeout_seconds` (95)
- Job retention limits `gateway.max_retained_jobs` (1000) and `gateway.max_retained_payload_bytes` (64 MiB) with automatic pruning of drained terminal jobs
- Transactional global-config updates through locked atomic file transactions (`internal/filetxn`) that never materialize environment credentials
- Per-model embedding target resolution on `/v1/embeddings`

### Changed
- CLI commands read invocation options from a per-run struct instead of package-level flag globals
- Routing publication uses immutable config snapshots while provider HTTP resources stay pooled across generations
- Canary auto-promotion runs as a coalescing, cancellable background evaluator and publishes through transactional config updates
- `models.dev` price sync rejects missing, NaN, infinite, or negative prices and matches provider filters case-insensitively; CSV price parsing rejects invalid values
- Catalog store and routing-metrics persistence reuse the shared atomic file-transaction helpers
- Gateway shutdown honors the configured shutdown timeout and request context cancellation

## [0.1.0] - 2026-07-31

### Added
- Cache-first `llambo evals` rankings that combine LLM Stats and Artificial Analysis through the deterministic, frozen-reference LES-1 formula
- External-evidence diagnostics for coverage, evidence bounds, source disagreement, identity confidence, reference drift, and jackknife rank stability
- Explicit local/OSS artifact projections onto reviewed LLM Stats or Artificial Analysis rows, with confidence caps and missing-source diagnostics
- Live loopback OMLX inventory discovery that selects reviewed text-capable local projections and reports unmatched or excluded models
- Model catalog selectors for `ping` and `prompt`, including `all`, `free`, `pinned`, `healthy`, `tag:<name>`, and `category:<name>`
- Free-model discovery workflow with catalog health persistence, 2-hour quarantine for bad or slow models, and optional pinning
- Catalog tag, pin, avoid, and free-listing documentation
- Prompt fanout with stdin support and cost-aware filters
- Native Gemini API support with client caching
- Jobs CLI for batch testing and benchmarking
- Native embeddings provider integration
- Handler timeouts and body size limits for gateway
- Partial failure support for job processing
- Auto-recovery for circuit breakers after cooldown period
- High-performance LLM gateway service with parallel job processing
- Intelligent multi-objective router with intent detection (`code`, `extraction`, `long_context`, `chat`) and policy modes (`quality`, `cheapest`, `fastest`, `balanced`)
- Rule-based route preferences via `~/.config/llambo/route-preferences.yaml` and `llambo route query` CLI
- Per-backend circuit breakers with automatic 60s to 5m failover recovery
- Multi-key rotation on 429 rate limit responses
- Streaming response engines for OpenAI and Anthropic compatible protocols
- Cache-first `llambo evals` framework using the LES-1 percentile formula
- Model catalog, health tracking, and zero-cost model discovery (`llambo models discover-free`)
- Native Gemini API support, embeddings endpoint, and batch job manager
- MIT Open Source License

### Changed
- `llambo evals` keeps external sources cache-only by default while using live loopback OMLX inventory selection; `--offline` disables all source access
- `llambo evals` now defaults to an overall-score eligibility cutoff of 40; `--min-overall -1` disables filtering
- `llambo evals` now excludes canonical models whose highest known output-token price exceeds $10 per 1M; unknown prices and local projections remain eligible
- Embeddings now document both single-string and array input
- Provider configuration docs now mark `api_path` as a legacy compatibility field
- Installation and contributing docs now use the Go version from `go.mod`
- Updated flow documentation with architecture audit
- Migrated from Bifrost SDK to native OpenAI Go SDK
- Enhanced provider types and gateway interfaces
- Improved SOLID compliance across codebase
- Added comprehensive test coverage
- Initial public release

### Fixed
- Job status reporting and error handling
- Circuit breaker auto-recovery logic
- Various provider integration issues

### Removed
- Local model evaluation harnesses, calibration anchors, ridge fitting, and local-score dependencies from the active ranking workflow
- Bifrost SDK dependencies and abstractions
- Unused code and legacy abstractions
---

## Types of Changes

- **Added** for new features
- **Changed** for changes in existing functionality
- **Fixed** for any bug fixes
- **Removed** for now removed features
- **Security** in case of vulnerabilities

This changelog format follows the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) specification.
