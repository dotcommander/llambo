# 🏎️ Llambo

**The high-performance, zero-friction LLM gateway.**  
*Fan out prompts across multiple AI backends in parallel, bypass rate limits automatically, and stream results back instantly.*

---

## ⚡ TL;DR: Why Llambo?

Stop getting blocked by single-provider rate limits, 429 errors, or slow sequential API calls while building.

| Problem | Standard Gateway | 🏎️ Llambo |
| :--- | :--- | :--- |
| **Speed** | Slow sequential requests | **Parallel fanout across multiple backends** |
| **Rate Limits** | 429 error crashes your script | **Auto-key rotation + instant failover** |
| **Circuit Breakers** | Manual retries & babysitting | **Automatic background health tracking & recovery** |
| **Output** | Wait for every model to finish | **Stream results in real-time as they complete** |
| **Model Selection** | Guessing pricing & latency | **Intelligent intent-based routing (`fastest`, `cheapest`, `quality`)** |

---

## 🚀 30-Second Quickstart

Get running in three commands:

```bash
# 1. Install & symlink to PATH
go install github.com/dotcommander/llambo@latest

# 2. Initialize default config
llambo config init

# 3. Start the gateway server
llambo serve
```

### 🎯 Instant CLI Magic

`llambo models --metrics` marks OMLX decode speed estimates with `~`. These use
an M2 Max 96 GiB hardware profile and model-name size/quantization hints; they
are estimates, not fresh measurements of the configured server.

```bash
# Discover & pin zero-cost models automatically
llambo models discover-free -P openrouter --pin

# Fan out one prompt to all healthy models in parallel
llambo prompt --models healthy "Explain quantum computing in one sentence"

# Multi-lens prompt fusion: query diverse models and synthesize the best answer
llambo prompt --models free --fuse "Design a microservice architecture for real-time chat"

# Query smart routing decisions for your prompt
llambo route query "Write a fast Go routine to parse JSON"

# Rank cached external evaluations by coding profile
llambo evals --rank-by coding

# Scrape writing benchmarks and list the latest open-weight model queue
llambo evals writing --refresh

# Export WritingBench prompts as normalized JSONL for model reruns
llambo evals writing --refresh --prompt-source writingbench --prompt-limit 20 --export-prompts /tmp/writingbench.jsonl

# Plan a bounded local writing run (zero provider calls without --execute)
llambo evals writing run --input /tmp/writingbench.jsonl --benchmark writingbench \
  --model deepseek/deepseek-v4.1-flash --judge-model openrouter/anthropic/claude-sonnet-5 \
  --output-dir /tmp/writing-run

# Find newly updated public text-generation candidates for review
llambo evals writing --refresh --discover-open-models --discover-limit 25

# Save a standalone sortable browser report
llambo evals --rank-by coding --format html --output /tmp/llambo-evals-coding.html
```

---

## 🔑 Key Features

- **🔥 Real-time Parallel Execution**: Send batch jobs across multiple providers concurrently. Poll completed results while the rest continue.
- **🛡️ Auto-Failover & Key Rotation**: Configured with multiple API keys? Llambo automatically rotates keys on `429 Too Many Requests`. If a provider goes down, requests seamlessly fail over to the next healthy backend.
- **🧠 Intelligent Intent Router**: Automatically classifies your prompt intent (`code`, `extraction`, `long_context`, `chat`) and picks the optimal backend based on your desired policy (`fastest`, `cheapest`, `quality`, or `balanced`).
- **📊 Cache-First Evals (`llambo evals`)**: Compare six independent LLAMBO-7 capability scores from frozen external benchmarks without refreshing sources. Rank by capability, speed, or price.
- **🔄 Familiar APIs**: Use OpenAI-compatible chat, models, and embeddings endpoints or the Anthropic-compatible Messages endpoint.

---

## 🛠️ Configuration

Config file location: `~/.config/llambo/config.json`

```json
{
  "default_provider": "openai",
  "providers": {
    "openai": {
      "provider_type": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-5.6-sol",
      "workers": 3,
      "priority": 1,
      "enabled": true
    },
    "openrouter": {
      "provider_type": "openrouter",
      "base_url": "https://openrouter.ai/api",
      "model": "anthropic/claude-3-5-sonnet",
      "workers": 2,
      "priority": 2,
      "enabled": true,
      "extra_headers": {
        "HTTP-Referer": "https://github.com/dotcommander/llambo"
      }
    }
  },
  "gateway": {
    "auth_token_env": "LLAMBO_GATEWAY_TOKEN",
    "allowed_origins": ["https://app.example.com"]
  },
  "routing": {
    "mode": "balanced",
    "max_cost_usd": 0.02,
    "max_latency_ms": 4000,
    "allowed_providers": ["openai", "openrouter"],
    "daily_max_cost_usd": {"*": 25.0}
  }
}
```

### Config Options

| Field | Description |
| :--- | :--- |
| `default_provider` | Required provider key used when a command omits a provider |
| `base_url` | Provider root URL (**no** `/v1` suffix for OpenAI-compatible providers) |
| `model` | Target model identifier |
| `api_keys` | Array of API keys rotated on 429 rate limits |
| `workers` | Max parallel request slots per backend |
| `priority` | Load balancing priority order (lower number = higher priority) |
| `capabilities` | Intent tags (e.g. `["chat", "code", "extraction"]`) |
| `gateway.auth_token_env` | Preferred environment variable containing the gateway bearer token |
| `gateway.allowed_origins` | Exact browser origins allowed by CORS; empty disables CORS |

Non-loopback serving requires `--allow-remote`. Set gateway authentication
before exposing Llambo beyond loopback.

---

## 🧠 Intelligent Routing Modes

Set `"mode"` in your `routing` config to control policy:

- **`fastest`**: Prefers low-latency backends (e.g. Groq, Cerebras).
- **`cheapest`**: Prefers low-cost per 1M tokens.
- **`quality`**: Prefers flagship/strong capability models.
- **`balanced`**: Optimal multi-objective blend of quality, fit, latency, cost, and reliability.

### Custom Rule Preferences (`route-preferences.yaml`)

Define exact phrase rules in `~/.config/llambo/route-preferences.yaml`:

```yaml
route_preferences:
  - name: code_heavy
    match:
      intent: code
      any_phrases: ["refactor", "goroutine", "debug"]
    choose:
      provider: openrouter
      model: anthropic/claude-3-5-sonnet
```

---

## 🔌 API Reference

### Endpoints Matrix

| Endpoint | Method | Description |
| :--- | :---: | :--- |
| `/v1/chat/completions` | `POST` | OpenAI-compatible chat completion with automatic failover |
| `/v1/messages` | `POST` | Anthropic-compatible messages endpoint |
| `/v1/embeddings` | `POST` | Vector embeddings generation |
| `/v1/jobs` | `POST` | Submit parallel batch jobs |
| `/v1/jobs/{id}` | `GET` | Poll batch job status and completed results |
| `/v1/models` | `GET` | List available models |
| `/health` | `GET` | Gateway health check & circuit breaker status |
| `/stats` | `GET` | Token usage and cost tracking statistics |

### Chat Completion Example

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.6-sol",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

Every response includes provider transparency metadata (`x_llambo` body field & `X-Llambo-Provider` headers):

```json
{
  "id": "chatcmpl-...",
  "choices": [...],
  "x_llambo": {
    "backend": "openai",
    "model": "gpt-5.6-sol",
    "duration_ms": 420,
    "total_tokens": 75,
    "cost_usd": 0.0003
  }
}
```

---

## 🛡️ Circuit Breaker & Health Recovery

Llambo automatically manages provider health in the background so you never get stuck:

| Trigger Event | Gateway Action | Cooldown Period |
| :--- | :--- | :--- |
| **HTTP 429** | Rotate key; disable provider if keys exhausted | 5 minutes |
| **Rate Limit / Quota in Error** | Mark provider quarantined | 5 minutes |
| **3+ Consecutive Failures** | Mark provider degraded | 60 seconds |
| **Successful Request** | Re-enable provider & reset failure counters | Immediate |

---

## 💡 Troubleshooting Cheat Sheet

- **HTTP 404 from Provider?**  
  *Fix:* Remove `/v1` from `base_url` in config (e.g. use `"https://api.openai.com"`, not `"https://api.openai.com/v1"`).
- **Changes Don't Take Effect?**  
  *Fix:* Always re-symlink after building locally: `go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo`.
- **Check Circuit Breaker Status?**  
  *Fix:* Run `curl http://localhost:8080/health` to view active backend health and `disabled_until` timestamps.

---

## 💻 Development & Building

```bash
# Build binary & symlink to PATH
go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo

# Run all tests
go test ./...
```

---

## 📜 License

Distributed under the [MIT License](LICENSE).
