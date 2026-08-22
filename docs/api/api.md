# API Reference

Complete reference for Llambo's HTTP API.

## Base URL

```
http://localhost:8080
```

Default port is `8080`. Configure with `--port` flag.

---

## Chat Completions

Create a chat completion with automatic failover across backends.

### Request

```http
POST /v1/chat/completions
Content-Type: application/json
```

```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "temperature": 0.7,
  "max_tokens": 1000
}
```

#### Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `model` | string | No | Model hint (backend's configured model is used) |
| `messages` | array | Yes | Conversation messages |
| `messages[].role` | string | Yes | `system`, `user`, or `assistant` |
| `messages[].content` | string | Yes | Message content |
| `temperature` | number | No | Sampling temperature (0-2) |
| `max_tokens` | number | No | Maximum tokens to generate |

### Response

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1704067200,
  "model": "gpt-4o",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! How can I help you today?"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 20,
    "completion_tokens": 10,
    "total_tokens": 30
  },
  "x_llambo": {
    "backend": "openai",
    "model": "gpt-4o",
    "duration_ms": 1234
  }
}
```

#### Response Fields

| Field | Description |
|-------|-------------|
| `id` | Unique completion identifier |
| `choices[].message.content` | Generated response |
| `choices[].finish_reason` | Why generation stopped: `stop`, `length`, `content_filter` |
| `usage` | Token counts (if provider returns them) |
| `x_llambo.backend` | Which backend served the request |
| `x_llambo.model` | Actual model used |
| `x_llambo.duration_ms` | Request duration in milliseconds |

### Error Response

```json
{
  "error": {
    "message": "all backends failed",
    "type": "server_error",
    "code": 500
  }
}
```

---

## Messages

Create Anthropic-compatible messages through Llambo's translation layer.

### Request

```http
POST /v1/messages
Content-Type: application/json
```

For detailed request and response examples, see [Messages](messages.md).

---

## Embeddings

Generate vector embeddings for text.

### Request

```http
POST /v1/embeddings
Content-Type: application/json
```

```json
{
  "input": "Hello world",
  "model": "text-embedding-3-small"
}
```

`input` may also be an array when embedding multiple strings in one request.

#### Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `input` | string/array | Yes | Text(s) to embed |
| `model` | string | No | Model hint; the configured embedding backend owns the effective model |

### Response

```json
{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "index": 0,
      "embedding": [0.0023, -0.0045, 0.0012, ...]
    },
    {
      "object": "embedding",
      "index": 1,
      "embedding": [0.0011, -0.0032, 0.0008, ...]
    }
  ],
  "model": "text-embedding-3-small",
  "usage": {
    "prompt_tokens": 4,
    "total_tokens": 4
  }
}
```

---

## Models

List available models from all enabled backends.

### Request

```http
GET /v1/models
```

### Response

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o",
      "object": "model",
      "owned_by": "openai"
    },
    {
      "id": "anthropic/claude-3-5-sonnet",
      "object": "model",
      "owned_by": "openrouter"
    }
  ]
}
```

---

## Jobs

Submit batch requests for parallel processing across all healthy backends.

### Create Job

```http
POST /v1/jobs
Content-Type: application/json
```

```json
{
  "system_prompt": "Be concise and helpful.",
  "requests": [
    {
      "id": "q1",
      "messages": [
        {"role": "user", "content": "What is the capital of France?"}
      ]
    },
    {
      "id": "q2",
      "messages": [
        {"role": "user", "content": "What is 2 + 2?"}
      ]
    },
    {
      "id": "q3",
      "messages": [
        {"role": "assistant", "content": "I'm a math tutor."},
        {"role": "user", "content": "Explain quadratic equations."}
      ]
    }
  ]
}
```

#### Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `system_prompt` | string | No | System prompt applied to all requests |
| `requests` | array | Yes | Individual requests to process |
| `requests[].id` | string | Yes | Unique identifier for this request |
| `requests[].messages` | array | Yes | Conversation messages |

#### Response

```json
{
  "job_id": "job-abc123",
  "status": "pending",
  "total_requests": 3
}
```

### Get Job Status

```http
GET /v1/jobs/{job_id}
```

#### Response (Processing)

```json
{
  "job_id": "job-abc123",
  "status": "processing",
  "total_requests": 3,
  "completed": 1,
  "failed": 0,
  "results": [
    {
      "id": "q2",
      "status": "completed",
      "content": "4",
      "backend": "openai",
      "model": "gpt-4o",
      "duration_ms": 456
    }
  ]
}
```

#### Response (Completed)

```json
{
  "job_id": "job-abc123",
  "status": "completed",
  "total_requests": 3,
  "completed": 3,
  "failed": 0,
  "results": [
    {
      "id": "q1",
      "status": "completed",
      "content": "The capital of France is Paris.",
      "backend": "openrouter",
      "model": "claude-3-5-sonnet",
      "duration_ms": 892
    },
    {
      "id": "q2",
      "status": "completed",
      "content": "4",
      "backend": "openai",
      "model": "gpt-4o",
      "duration_ms": 456
    },
    {
      "id": "q3",
      "status": "completed",
      "content": "Quadratic equations are polynomial equations of degree 2...",
      "backend": "openai",
      "model": "gpt-4o",
      "duration_ms": 1234
    }
  ]
}
```

#### Job Statuses

| Status | Description |
|--------|-------------|
| `pending` | Job created, queued for processing |
| `processing` | At least one request has started |
| `completed` | All requests finished (check `failed` count) |
| `failed` | All requests failed |
| `cancelled` | Job was cancelled |

#### Result Statuses

| Status | Description |
|--------|-------------|
| `completed` | Request succeeded |
| `failed` | Request failed (see `error` field) |

### Cancel Job

```http
POST /v1/jobs/{job_id}/cancel
```

#### Response

```json
{
  "job_id": "job-abc123",
  "status": "cancelled"
}
```

**Note:** Cancellation is best-effort. In-flight requests may still complete.

---

## Health Check

See [Operational Endpoints](operations.md#health-check) for detailed health check documentation including backend status and circuit breaker states.

---

## Providers

See [Operational Endpoints](operations.md#providers) for detailed provider documentation including configuration and health status.

---

## Stats

See [Operational Endpoints](operations.md#stats) for detailed token usage and cost statistics documentation.

---

## Error Handling

All errors follow this format:

```json
{
  "error": {
    "message": "descriptive error message",
    "type": "error_type",
    "code": 400
  }
}
```

### Error Types

| Type | HTTP Code | Description |
|------|-----------|-------------|
| `invalid_request_error` | 400 | Malformed request |
| `not_found` | 404 | Job not found |
| `rate_limit_error` | 429 | All backends rate limited |
| `server_error` | 500 | Internal error or all backends failed |

---

## Rate Limits

Llambo doesn't impose its own rate limits. However:

- Backend rate limits are respected
- Circuit breaker disables backends hitting rate limits
- Jobs distribute across backends to avoid concentrated load

To handle rate limits:

1. Configure multiple backends
2. Set appropriate `workers` count per backend
3. Use batch jobs for large workloads (automatic distribution)
