# Messages

Create Anthropic-compatible messages via a translation layer.

## Endpoint

```http
POST /v1/messages
Content-Type: application/json
```

## Overview

This endpoint accepts Anthropic Messages API-style payloads, translates them to Llambo's internal chat flow, and maps the response back to Anthropic-style JSON.

For non-text input blocks (`image`, `document`, `tool_use`, `tool_result`), translation is best-effort and converted to textual context for the internal chat flow.

## Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `model` | string | No | Model hint; configured backend model is used at execution time |
| `max_tokens` | integer | Yes | Maximum output tokens; must be greater than 0 |
| `messages` | array | Yes | Conversation turns (`user` and `assistant` roles only) |
| `messages[].content` | string or array | Yes | String or block array (`text`, `image`, `document`, `tool_use`, `tool_result`) |
| `system` | string or array | No | Top-level system instruction (Anthropic style) |
| `temperature` | number | No | Sampling temperature |
| `top_p` | number | No | Nucleus sampling value (forwarded when supported) |
| `top_k` | integer | No | Forwarded as raw JSON override (`top_k`) when provider supports it |
| `metadata` | object | No | Request metadata; forwarded when provider supports chat metadata |
| `thinking` | object | No | Validated as object and forwarded as raw JSON override (`thinking`) when provider supports it |
| `stream` | boolean | No | Stream Anthropic-style SSE events (`message_start`, `content_block_*`, `message_delta`, `message_stop`) |
| `stop_sequences` | array[string] | No | Stops response at first matched sequence and returns `stop_reason: "stop_sequence"` |
| `tools` | array | No | Tool definitions for provider-native tool calling |
| `tool_choice` | string or object | No | Tool choice policy: `auto`, `none`, `any`, or `{type:"tool",name:"..."}` |

Validation limits:

- `messages` max: `10000`
- content blocks per `content` array max: `5000`
- `stop_sequences` max: `256`
- metadata pairs max: `16` (keys truncated to `64` chars, values truncated to `512` chars)

Tool block required fields:

- `tool_use` blocks require `id` and `name`
- `tool_result` blocks require `tool_use_id`

## Example Request

```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet",
    "max_tokens": 256,
    "system": "You are a concise assistant.",
    "messages": [
      {"role": "user", "content": "Summarize HTTP in one paragraph."}
    ]
  }'
```

## Tool Round-Trip Example

Tool call response from `/v1/messages` (model requests tool execution):

```json
{
  "id": "msg_abc123",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "tool_use",
      "id": "call_1",
      "name": "search",
      "input": {"query": "llambo architecture"}
    }
  ],
  "model": "gpt-4o",
  "stop_reason": "tool_use",
  "stop_sequence": null,
  "usage": {"input_tokens": 120, "output_tokens": 18}
}
```

Follow-up request that returns the tool result back into `/v1/messages`:

```json
{
  "model": "claude-3-5-sonnet",
  "max_tokens": 256,
  "messages": [
    {
      "role": "assistant",
      "content": [
        {
          "type": "tool_use",
          "id": "call_1",
          "name": "search",
          "input": {"query": "llambo architecture"}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "call_1",
          "content": [{"type": "text", "text": "Found docs/architecture/request-flow.md"}]
        }
      ]
    }
  ]
}
```

## Success Response

```json
{
  "id": "msg_abcd1234",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "text",
      "text": "HTTP is the protocol that powers data exchange on the web..."
    }
  ],
  "model": "gpt-4o-mini",
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {
    "input_tokens": 43,
    "output_tokens": 29
  }
}
```

## Streaming Response (SSE)

With `"stream": true`, the endpoint returns `text/event-stream` events.

```text
event: message_start
data: {"type":"message_start","message":{"id":"msg_...","type":"message","role":"assistant","content":[],"model":"claude-3-5-sonnet","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":12}}

event: message_stop
data: {"type":"message_stop"}
```

When the upstream model emits tool calls, `content` may include `tool_use` blocks.

Example mixed response with both explanatory text and a tool call:

```json
{
  "id": "msg_mixed123",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "text",
      "text": "I should look that up before answering."
    },
    {
      "type": "tool_use",
      "id": "call_2",
      "name": "search",
      "input": {"query": "llambo message translation"}
    }
  ],
  "model": "gpt-4o",
  "stop_reason": "tool_use",
  "stop_sequence": null,
  "usage": {"input_tokens": 101, "output_tokens": 24}
}
```

## Error Response

Errors follow Anthropic-style envelope and include `request_id`.

```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "messages[0].content[0]: type \"video\" is not supported"
  },
  "request_id": "req_1234abcd"
}
```

## Current Limitations (Phase 2)

- Block translation is best-effort for non-text content (placeholders/summaries are used).
- Final assistant prefill messages are translated in best-effort mode.
- Streaming event payloads are Anthropic-compatible but currently optimized for text and tool deltas (advanced event variants may be best-effort).
- `stop_reason` is mapped from provider finish reasons when available.
- `stop_sequences` are forwarded to providers when supported; Llambo also enforces them at response mapping time for compatibility.
- Streaming also enforces `stop_sequences` with chunk-boundary-safe matching.
- `stop_sequences` are normalized before use: empty values are dropped and duplicates are removed (first occurrence kept).
- `tools` and `tool_choice` are forwarded when provider supports OpenAI-style function tools.
- `top_k` and `thinking` are forwarded as raw JSON overrides; providers may ignore unsupported fields.
- Response guardrails: assistant text is capped at `100000` runes, and `tool_use` blocks are capped at `128` per response.

## Tool Result Normalization

Incoming `tool_result.content` is normalized into internal text context using this order:

1. String content is used as-is.
2. Content-block arrays are flattened to text in block order.
3. Other JSON values are compacted to JSON text.

This keeps tool round-trips resilient across providers while preserving useful context for the next model turn.

## Tool Loop Flow

Typical tool loop with `/v1/messages`:

1. User asks a question requiring external data.
2. Assistant response returns `tool_use` block(s) with tool name and input.
3. Client executes tool(s) and sends a follow-up request containing `tool_result` block(s).
4. Assistant returns final text answer (or additional `tool_use` blocks if more steps are needed).

This endpoint supports repeated tool loops across multiple turns.

## Compatibility Matrix

| Area | Status | Notes |
|------|--------|-------|
| Core request/response envelope | Exact | Anthropic-style request body and response envelope |
| Streaming SSE event flow | Exact (core) | `message_start`, `content_block_*`, `message_delta`, `message_stop`, `error` |
| Stop sequence behavior | Exact (pragmatic) | Enforced in sync + stream, including stream chunk boundary-safe matching |
| Tools/tool_choice | Exact (core) | Supports `auto`, `none`, `any`, and named `tool`; validates required fields |
| Tool round-trip blocks | Best-effort+core | `tool_use` emitted; incoming non-text tool content normalized for context |
| Image/document input blocks | Best-effort | Converted to textual placeholders for internal flow |
| `top_k`/`thinking` | Best-effort+forwarded | Forwarded as raw JSON overrides; provider behavior depends on backend support |
| Metadata forwarding | Best-effort+core | Metadata normalized and forwarded where provider supports metadata |

## Response Headers

| Header | Description |
|--------|-------------|
| `request-id` | Anthropic-style request identifier |
| `X-Llambo-Provider` | Backend that served the request |
| `X-Llambo-Model` | Model used for completion |
| `anthropic-version` | Version header (mirrors request header or defaults to `2023-06-01`) |

Strict mode:

- Set `LLAMBO_ANTHROPIC_STRICT=true` to require incoming `anthropic-version` header.
- In strict mode, unknown JSON request fields are rejected (`400 invalid_request_error`).
