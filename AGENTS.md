# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# ALWAYS use this - builds AND symlinks to PATH
go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo

# Or just run without building
go run . <command>
```

**CRITICAL**: Always symlink after building. User runs `llambo` from PATH, not `./llambo`.

## Purpose

Llambo (LLM + Lamborghini) is a **high-performance LLM gateway service** with parallel job processing.

**Key features:**
- Real-time parallel job processing (not async batch API)
- Per-backend circuit breakers
- Streaming results as jobs complete
- Cross-backend load balancing per request

## Commands

```bash
# Start gateway server
llambo serve [--port 8080] [--host 127.0.0.1] [--allow-remote]

# Configuration
llambo config init       # Create ~/.config/llambo/config.json
llambo config show       # Display current config

# Provider and model catalog
llambo providers refresh [provider]
llambo models catalog [--free] [--tag smart]
llambo models catalog tag <provider> <model> <tag>
llambo models catalog untag <provider> <model> <tag>
llambo models discover-free [-P provider] [--pin]

# Live health checks and prompt fanout
llambo ping --models healthy [-P provider]
llambo prompt --models tag:smart "Prompt text"
printf 'Prompt text\n' | llambo prompt --models free
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | OpenAI-compatible chat (single request with failover) |
| `/v1/messages` | POST | Anthropic-compatible messages (translated to internal chat flow) |
| `/v1/embeddings` | POST | Generate embeddings |
| `/v1/models` | GET | List available models |
| `/v1/jobs` | POST | Submit batch job for parallel processing |
| `/v1/jobs/{id}` | GET | Get job status and results |
| `/v1/jobs/{id}/cancel` | POST | Cancel running job |
| `/health` | GET | Health check with backend status |
| `/providers` | GET | List configured providers |
| `/stats` | GET | Token usage and cost statistics |

## Response Headers

Chat completion responses include provider transparency headers:

| Header | Description |
|--------|-------------|
| `X-Llambo-Provider` | Backend that served the request (e.g., `openai`, `openrouter`) |
| `X-Llambo-Model` | Model used for completion |

## Architecture

```
cmd/                              # Kong commands, selection, fanout, reporting
  {config,csv,evals,jobs,jobs_results,jobs_stress_results}.go
  {models,models_catalog,models_catalog_labels,models_discover_free,models_sync_pricing}.go
  {parallel,ping,ping_gemini,ping_metrics,ping_openai,ping_output,ping_targets}.go
  {prompt,prompt_execution,prompt_fuse,prompt_fuse_logic,prompt_output}.go
  {provider_config,providers,root,root_commands_models,root_commands_runtime,serve}.go
  {route,route_canary,route_preferences,route_report_helpers}.go

internal/
  catalog/                        # Persisted model catalog, refresh, selection, quality
    {capability,catalog,fetcher,health,pricing,provider_fetchers}.go
    {quality,refresh,selector,selector_match,tags}.go
  costs/                          # Price-file and models.dev cost loading
    {costs,modelsdev}.go
  evals/                          # LES-1 source fetch, scoring, projections, reports
    {cache,fetch,fetch_sources,filter,formula_config,formula_math}.go
    {formula_score,formula_stability,merge,omlx,projections}.go
    {reference,render,report,types}.go
  gateway/                        # HTTP API, protocol translation, streaming, jobs
    {anthropic,anthropic_handler,anthropic_request,anthropic_response}.go
    {anthropic_stream,anthropic_stream_state,anthropic_tools,constants}.go
    {handlers,http_responses,job,job_handlers,job_manager,messages}.go
    {openai_stream,server,status_handlers,types}.go
  modelsdev/                      # models.dev fetch, normalization, config synchronization
    {fetch,modelsdev,sync}.go
  styles/styles.go                # Shared CLI presentation styles

providers/                        # Provider protocol, routing, retry, telemetry, queues
  {backoff,blocklist,canary,chat_execution,chat_execution_attempt}.go
  {chat_execution_canary,chat_execution_outcomes,chat_request,chat_request_build}.go
  {chat_response,chat_stream,circuit_breaker,client_factory,client_provider}.go
  {config,config_providers,constants,context_precheck,cost_tracker,embeddings}.go
  {errors,errors_classification,errors_patterns,errors_types,executor,gateway_config}.go
  {intelligent_router,key_rotator,openai_provider,provider,queue,registry}.go
  {request_options,response,retry_after,retry_headers_transport}.go
  {routing_estimates,routing_events,routing_metrics}.go
```

## Config-Driven Providers

All provider config comes from `~/.config/llambo/config.json`:

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
    },
    "openrouter": {
      "base_url": "https://openrouter.ai/api",
      "model": "anthropic/claude-3-5-sonnet",
      "workers": 2,
      "priority": 2,
      "enabled": true
    }
  }
}
```

**Adding a new provider = just edit config.json.** No code changes needed.

## Config Fields

| Field | Required | Description |
|-------|----------|-------------|
| `base_url` | Yes | API endpoint (NO `/v1` suffix) |
| `model` | Yes | Model identifier |
| `enabled` | Yes | Whether backend is active |
| `api_keys` | No | Array of API keys (rotates on 429) |
| `env_var` | No | Env var for single API key (fallback if no `api_keys`) |
| `workers` | No | Parallel request slots (default: 2) |
| `priority` | No | Load balancing order (lower = higher priority) |
| `max_tokens` | No | Max completion tokens |
| `extra_headers` | No | Provider-specific headers |
| `requires_key` | No | Whether API key required (default: true) |
| `models` | No | Optional provider model list used by catalog refresh and routing helpers |
| `extra_body` | No | Provider request body fields passed through to wormhole |
| `extra_body_by_model` | No | Per-model provider request body overrides |
| `api_path` | No | Legacy compatibility field; serving path ignores it |
| `gateway.auth_token_env` | No | Preferred environment variable containing the gateway bearer token |
| `gateway.auth_token` | No | Direct bearer token; prefer `auth_token_env` |
| `gateway.allowed_origins` | No | Exact browser origins allowed to call the gateway; CORS is disabled when empty |

Non-loopback `serve --host` values require `--allow-remote`. Configure gateway
bearer authentication before exposing the listener beyond loopback.

## Circuit Breaker & Key Rotation

**Key rotation** (when `api_keys` configured):

| Trigger | Action |
|---------|--------|
| HTTP 429 | Rotate to next key, retry same provider |
| All keys exhausted | Circuit-break provider |
| Key cooldown expires | Key becomes available again |

**Circuit breaker** (per-backend):

| Trigger | Action | Cooldown |
|---------|--------|----------|
| HTTP 429 (no keys left) | Immediate disable | 5 minutes |
| "rate limit" in error | Immediate disable | 5 minutes |
| "quota" in error | Immediate disable | 5 minutes |
| 3+ consecutive failures | Disable | 60 seconds |
| Success after cooldown | Re-enable, reset counter | - |

With 3 API keys per provider, you get 3x the rate limit before failover.

## Job Processing

Jobs are processed in parallel across all healthy backends:

```bash
# Submit batch job
curl -X POST http://localhost:8080/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "system_prompt": "Be concise.",
    "requests": [
      {"id": "1", "messages": [{"role": "user", "content": "Question 1"}]},
      {"id": "2", "messages": [{"role": "user", "content": "Question 2"}]}
    ]
  }'

# Check status
curl http://localhost:8080/v1/jobs/job-abc123
```

Results stream back as they complete - no waiting for all jobs to finish.

## Common Issues

### HTTP 404 from OpenAI-compatible provider
**Cause**: Double `/v1` in URL. The SDK appends `/v1/chat/completions` to BaseURL.
**Fix**: Remove `/v1` from `base_url` in config.

### Provider uses wrong BaseURL
**Cause**: Each backend needs its own OpenAI client.
**Fix**: This is handled automatically - each backend gets dedicated client.

### Changes don't take effect after build
**Cause**: Binary built to `./llambo` but user runs `~/go/bin/llambo`.
**Fix**: Always symlink: `ln -sf $(pwd)/llambo ~/go/bin/llambo`

## Key Implementation Details

### Per-Backend Wormhole Providers

Each backend gets its own wormhole provider so protocol execution stays at the leaf while llambo keeps routing, key rotation, circuit breakers, queues, and cost tracking:

```go
for _, entry := range FilterEnabledProviders(configs) {
    provider, err := createProviderForConfigWithKey(entry.Name, entry.Config, keyRotator.GetKey(entry.Name))
    clients[entry.Name] = provider
}
```

Every wormhole provider is constructed with `MaxRetries: 0`; llambo owns retry, failover, key rotation, and circuit breaker state.

### API Paths

OpenAI-compatible wormhole providers derive request paths from normalized `base_url`
and append `/chat/completions`. The legacy `api_path` config field is kept for
config compatibility but is not read by the serving path.

## Operational Context

- Existing ping CLI entry point is `cmd/ping.go`; it already runs providers in parallel and outputs latency/token stats.
- `cmd/model_selector.go` owns `--models` selector resolution for ping and prompt; `free` means explicit `0,0` pricing in `~/.config/llambo/model-costs.csv`, not unknown price.
- `cmd/prompt.go` accepts prompt text as a positional arg or from stdin and best-effort records model health in the catalog.
- `cmd/models.go` owns catalog tag, pin, avoid, refresh, and `discover-free` commands; health/quarantine state is persisted in `~/.config/llambo/catalog.json`.
- `llambo ping` in this repo commonly exercises 7 configured providers (openai, openrouter, gemini, zai, synthetic, cerebras, groq) when user config is populated.
- Gateway chat, streaming chat, Gemini-native chat, embeddings, and OpenAI-compatible ping calls delegate protocol execution to the published `github.com/garyblankenship/wormhole` module; do not use local `replace` paths. OpenAI Responses support requires wormhole `v1.13.0` or newer.
- `llambo evals` is the sole active evaluation entrypoint. It defaults to cache-only operation and transforms LLM Stats and Artificial Analysis metrics with the frozen-reference LES-1 external percentile formula; only `--refresh` accesses the network, `--rank-by` selects the task profile, and the default eligibility filters hide rows below `--min-overall 40` or above the known `--max-output-price 10` after scoring. Unknown hosted prices and local projections remain price-eligible. Reviewed local/OSS artifacts are explicit projections applied after canonical scoring, so they never change the reference population, drift, or jackknife calculations.
- Historical local-eval notes and `.work` results are provenance only. No active scorer, test, or ranking may read or fit against them; do not revive the retired harnesses.
- `extra_body` passthrough stays on wormhole request provider options; wormhole `ProviderConfig.UseResponsesAPI` is used only for first-party OpenAI base URLs.

## Documentation Style & Writing Guidelines (ADHD-Friendly)

All writing for `README.md` and files under `docs/` MUST adhere to ADHD-friendly presentation standards:
- **Instant Scannability**: Use high-contrast Markdown tables, bold headers, and structural bullet points. Avoid dense walls of text.
- **Copy-Paste Quickstarts**: Provide concise 30-second runnable code snippets up front.
- **Punchy Visual Hierarchy**: Use clear section icons/emojis, TL;DR summaries, and immediate value callouts.
- **Zero Cognitive Friction**: Address common developer traps directly with actionable Troubleshooting cheat sheets.

