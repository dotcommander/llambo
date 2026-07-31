# Chat Completions

Create chat completions with automatic failover across multiple LLM providers.

## Endpoint

```http
POST /v1/chat/completions
Content-Type: application/json
```

## Overview

The chat completions endpoint provides OpenAI-compatible chat functionality with automatic failover across configured backends. When a request is received, Llambo routes it to the highest-priority healthy backend. If that backend fails, it automatically retries with the next available backend.

If you need Anthropic-compatible request and response envelopes, see [Messages](messages.md).

## Request Body Schema

### ChatCompletionRequest

| Field | Type | Required | Description | Constraints |
|-------|------|----------|-------------|-------------|
| `model` | string | No | Model identifier hint | Any string; backend's configured model is actually used |
| `messages` | array[Message] | **Yes** | Array of conversation messages | Minimum 1 message |
| `temperature` | float64 | No | Sampling temperature | 0.0 to 2.0, defaults to 1.0 |
| `max_tokens` | integer | No | Maximum tokens to generate | Positive integer |
| `stream` | boolean | No | Whether to stream response | Defaults to `false` (streaming not yet implemented) |

### Message Object

| Field | Type | Required | Description | Allowed Values |
|-------|------|----------|-------------|----------------|
| `role` | string | **Yes** | Role of the message author | `system`, `user`, `assistant` |
| `content` | string | **Yes** | Content of the message | Any non-empty string |

## Examples

### Basic Request

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Hello, how are you?"}
    ]
  }'
```

### Request with Parameters

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [
      {"role": "system", "content": "You are a technical expert."},
      {"role": "user", "content": "Explain quantum computing in simple terms."}
    ],
    "temperature": 0.7,
    "max_tokens": 500
  }'
```

### Multi-turn Conversation

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "system", "content": "You are a math tutor."},
      {"role": "user", "content": "What is 2 + 2?"},
      {"role": "assistant", "content": "2 + 2 equals 4."},
      {"role": "user", "content": "What about 3 + 3?"}
    ]
  }'
```

## Response

### Success Response (HTTP 200)

#### Response Body

```json
{
  "id": "chatcmpl-abc123def456",
  "object": "chat.completion",
  "created": 1704067200,
  "model": "gpt-4o",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! I'm doing well, thank you for asking. How can I assist you today?"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 25,
    "completion_tokens": 18,
    "total_tokens": 43
  },
  "x_llambo": {
    "backend": "openai",
    "model": "gpt-4o",
    "duration_ms": 1234,
    "prompt_tokens": 25,
    "completion_tokens": 18,
    "total_tokens": 43,
    "cost_usd": 0.0012
  }
}
```

#### Response Headers

| Header | Description | Example |
|--------|-------------|---------|
| `Content-Type` | Response format | `application/json` |
| `X-Llambo-Provider` | Backend that served the request | `openai` |
| `X-Llambo-Model` | Actual model used | `gpt-4o` |

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique completion identifier prefixed with `chatcmpl-` |
| `object` | string | Always `"chat.completion"` |
| `created` | integer | Unix timestamp of creation |
| `model` | string | Model used for completion |
| `choices` | array[Choice] | Array of completion choices (always 1 choice) |
| `choices[].index` | integer | Index of the choice (always 0) |
| `choices[].message` | Message | The generated message |
| `choices[].message.role` | string | Always `"assistant"` |
| `choices[].message.content` | string | Generated response content |
| `choices[].finish_reason` | string | Reason generation stopped: `"stop"`, `"length"`, or `"content_filter"` |
| `usage` | Usage | Token usage information (if provider returns it) |
| `usage.prompt_tokens` | integer | Number of tokens in the prompt |
| `usage.completion_tokens` | integer | Number of tokens in the completion |
| `usage.total_tokens` | integer | Total tokens (prompt + completion) |
| `x_llambo` | LlamboMeta | Llambo-specific metadata |
| `x_llambo.backend` | string | Which backend served the request |
| `x_llambo.model` | string | Actual model used |
| `x_llambo.duration_ms` | integer | Request duration in milliseconds |
| `x_llambo.prompt_tokens` | integer | Prompt tokens (if available) |
| `x_llambo.completion_tokens` | integer | Completion tokens (if available) |
| `x_llambo.total_tokens` | integer | Total tokens (if available) |
| `x_llambo.cost_usd` | float | Estimated cost in USD (if available) |

### Error Responses

#### Invalid Request (HTTP 400)

```json
{
  "error": {
    "message": "Messages array is required",
    "type": "invalid_request",
    "code": "missing_messages"
  }
}
```

#### Backend Failure (HTTP 502)

```json
{
  "error": {
    "message": "All backends failed: openai: rate limit exceeded, openrouter: quota exceeded",
    "type": "upstream_error",
    "code": "all_backends_failed"
  }
}
```

#### Streaming Not Implemented (HTTP 501)

```json
{
  "error": {
    "message": "Streaming not yet implemented",
    "type": "not_implemented",
    "code": "streaming_unsupported"
  }
}
```

## Headers

### Request Headers

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | Yes | Must be `application/json` |
| `Authorization` | No | Not required - API keys are configured server-side |

### Response Headers

| Header | Always Present | Description |
|--------|----------------|-------------|
| `Content-Type` | Yes | `application/json` |
| `X-Llambo-Provider` | When successful | Name of the backend that served the request |
| `X-Llambo-Model` | When successful | Model identifier used for the completion |

## Constraints and Limits

### Request Size
- Maximum request body size: **10MB**
- Minimum messages: **1 message**
- Maximum messages: Limited by request size

### Timeouts
- Handler timeout: **90 seconds**
- Server write timeout: **120 seconds**

### Token Limits
- Token limits are enforced by individual backends
- `max_tokens` parameter is passed to the backend if provided

## Behavior Notes

### Model Parameter
The `model` field in the request is treated as a hint. The actual model used is determined by the backend's configuration in `~/.config/llambo/config.json`.

### Backend Selection
1. Requests are routed to the highest-priority healthy backend
2. If the selected backend fails, the request is retried with the next healthy backend
3. If all backends fail, a 502 error is returned

### System Prompt Extraction
System prompts are automatically extracted from messages with `role: "system"`. Multiple system messages are concatenated.

### Failover Transparency
- The `X-Llambo-Provider` header indicates which backend served the request
- The `x_llambo.backend` field in the response body provides the same information
- This allows clients to know which provider was used for each request

## Common Use Cases

### Simple Chat
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "Tell me a joke"}
    ]
  }'
```

### Technical Assistance
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "system", "content": "You are a senior software engineer."},
      {"role": "user", "content": "How do I implement a binary search tree in Go?"}
    ],
    "temperature": 0.3,
    "max_tokens": 1000
  }'
```

### Creative Writing
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "system", "content": "You are a creative writing assistant."},
      {"role": "user", "content": "Write a short poem about the ocean."}
    ],
    "temperature": 0.9
  }'
```

## Integration Notes

### OpenAI SDK Compatibility
The endpoint is compatible with the OpenAI SDK. Use the base URL `http://localhost:8080/v1` (note: **no trailing slash**).

```python
import openai

client = openai.OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="dummy"  # API key configured server-side
)

response = client.chat.completions.create(
    model="gpt-4o",  # Hint only, actual model from config
    messages=[
        {"role": "user", "content": "Hello!"}
    ]
)
```

### Checking Provider Headers
To determine which backend served a request:

```javascript
fetch('http://localhost:8080/v1/chat/completions', {
  method: 'POST',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({
    messages: [{role: 'user', content: 'Hello'}]
  })
})
.then(response => {
  const provider = response.headers.get('X-Llambo-Provider');
  const model = response.headers.get('X-Llambo-Model');
  console.log(`Served by ${provider} using ${model}`);
  return response.json();
});
```
