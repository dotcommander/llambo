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

# Save a standalone sortable browser report
llambo evals --rank-by coding --format html --output /tmp/llambo-evals-coding.html
```

---

## 🔑 Key Features

- **🔥 Real-time Parallel Execution**: Send batch jobs across multiple providers concurrently. Results stream back the second each backend completes.
- **🛡️ Auto-Failover & Key Rotation**: Configured with multiple API keys? Llambo automatically rotates keys on `429 Too Many Requests`. If a provider goes down, requests seamlessly fail over to the next healthy backend.
- **🧠 Intelligent Intent Router**: Automatically classifies your prompt intent (`code`, `extraction`, `long_context`, `chat`) and picks the optimal backend based on your desired policy (`fastest`, `cheapest`, `quality`, or `balanced`).
- **📊 Cache-First Evals (`llambo evals`)**: Rank models using the deterministic LES-1 benchmark formula without refreshing external sources. Filter by coding, writing, speed, or cost limits instantly.
- **🔄 Drop-In Compatibility**: 100% compatible with OpenAI (`/v1/chat/completions`), Anthropic Messages (`/v1/messages`), and Embeddings (`/v1/embeddings`).

---

## 🛠️ Configuration

Config file location: `~/.config/llambo/config.json`

```json
{
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
      "model": "~anthropic/claude-sonnet-latest",
      "workers": 2,
      "priority": 2,
      "enabled": true,
      "extra_headers": {
        "HTTP-Referer": "https://github.com/dotcommander/llambo"
      }
    }
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
| `base_url` | API endpoint (**no** `/v1` suffix — SDK appends it automatically) |
| `model` | Target model identifier |
| `api_keys` | Array of API keys rotated on 429 rate limits |
| `workers` | Max parallel request slots per backend |
| `priority` | Load balancing priority order (lower number = higher priority) |
| `capabilities` | Intent tags (e.g. `["chat", "code", "extraction"]`) |

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
      model: ~anthropic/claude-sonnet-latest
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
| `/v1/jobs/{id}` | `GET` | Fetch batch job status and streaming results |
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
