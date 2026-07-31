# Practical Examples

Three high-value workflows you can run with Llambo today.

## 1) Run Llambo as a Smart Gateway for Your App

Use one OpenAI-compatible endpoint while Llambo handles provider selection and failover.

### Start the gateway

```bash
llambo serve
```

### Point your app/client to Llambo

```bash
curl -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [
      {"role": "system", "content": "Be concise."},
      {"role": "user", "content": "Give me 3 launch checklist items."}
    ]
  }'
```

### Inspect routing choice and backend transparency

- Response body `x_llambo` includes backend, model, duration, tokens, and cost.
- Response headers include:
  - `X-Llambo-Provider`
  - `X-Llambo-Model`

### Why this is useful

- Keep your app integration simple (single endpoint).
- Gain automatic failover and health-aware routing without changing app code.

## 2) Benchmark Routing Policy with Replay/Backtest

Replay real routing events and compare policy modes before changing production behavior.

### Generate events

Run normal traffic through `llambo serve`; route events are recorded to `routing.events_path`.

### Compare modes and generate a report

```bash
llambo route simulate \
  --modes fastest,cheapest,balanced,quality \
  --report ./routing-backtest.md
```

### Optional scoped replay

```bash
llambo route simulate \
  --from ~/.config/llambo/routing-events.jsonl \
  --limit 500 \
  --report ./routing-backtest.md
```

### Why this is useful

- Decide routing mode with evidence, not guesswork.
- See estimated latency/cost tradeoffs and provider distribution before rollout.

## 3) Process High-Volume Work with Parallel Jobs

Send many requests in one batch and get per-item completion status and timing.

### Submit a job

```bash
curl -X POST http://127.0.0.1:8080/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "system_prompt": "Answer in one sentence.",
    "requests": [
      {"id": "q1", "messages": [{"role": "user", "content": "Summarize feature flags."}]},
      {"id": "q2", "messages": [{"role": "user", "content": "Summarize circuit breakers."}]},
      {"id": "q3", "messages": [{"role": "user", "content": "Summarize key rotation."}]}
    ]
  }'
```

### Poll job status

```bash
curl http://127.0.0.1:8080/v1/jobs/<job_id>
```

### Why this is useful

- Great for extraction/evaluation/report pipelines.
- Parallel execution improves throughput and shortens total wall-clock time.
