# Models

List available models from all configured providers.

## Endpoint

```http
GET /v1/models
Content-Type: application/json
```

## Overview

The models endpoint returns enabled runtime backends from the loaded configuration. Each entry includes the model identifier and the provider/backend name that owns it.

If `routing.catalog_models` is set to `pinned`, Llambo expands pinned catalog
models into individual runtime backends before the server starts; those expanded
model entries are what `/v1/models` returns.

This endpoint follows OpenAI's `/v1/models` API format for compatibility with OpenAI SDKs and tools.

## Response Schema

### ModelsResponse

| Field | Type | Description |
|-------|------|-------------|
| `object` | string | Always `"list"` |
| `data` | array[ModelInfo] | Array of model objects |

### ModelInfo Object

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Model identifier (from provider configuration) |
| `object` | string | Always `"model"` |
| `owned_by` | string | Provider name that owns this model |

## Examples

### Basic Request

```bash
curl -X GET http://localhost:8080/v1/models
```

### Response with Multiple Providers

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
      "id": "claude-3-5-sonnet",
      "object": "model",
      "owned_by": "openrouter"
    },
    {
      "id": "gemini-2.0-flash",
      "object": "model",
      "owned_by": "gemini"
    }
  ]
}
```

### Response with Single Provider

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o",
      "object": "model",
      "owned_by": "openai"
    }
  ]
}
```

## Response

### Success Response (HTTP 200)

#### Response Body

The response body follows OpenAI's models list format with the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `object` | string | Always `"list"` |
| `data` | array[ModelInfo] | Array of model objects sorted by provider priority |

#### ModelInfo Object Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Model identifier as configured in `~/.config/llambo/config.json` |
| `object` | string | Always `"model"` |
| `owned_by` | string | Provider name (e.g., `"openai"`, `"openrouter"`) |

### Error Responses

No specific error responses for this endpoint. Returns HTTP 200 with empty array if no providers are configured or enabled.

## Headers

### Request Headers

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | No | Not required for GET requests |

### Response Headers

| Header | Always Present | Description |
|--------|----------------|-------------|
| `Content-Type` | Yes | `application/json` |

## Constraints and Limits

### Model Listing
- Only lists models from **enabled** providers
- Providers are sorted by their configured priority (lower priority number = higher priority)
- Each enabled runtime backend contributes one model entry
- Model IDs come from provider configuration (`model` field in config), including catalog-expanded pinned models when enabled

### Performance
- No upstream API calls - models are listed from local configuration
- Response time is minimal (just reading from memory)

## Behavior Notes

### Provider Filtering
Only providers with `enabled: true` in their configuration are included in the models list. Disabled providers are omitted.

### Priority Ordering
Models are listed in provider priority order (lowest priority number first). This matches the order used for failover in chat completions.

### Configuration-Driven
Model IDs come from the `model` field in each loaded runtime backend. Restart the server after changing `~/.config/llambo/config.json` or pinned catalog state so the server rebuilds its backend list.

### OpenAI Compatibility
The response format matches OpenAI's `/v1/models` endpoint for SDK compatibility. This allows OpenAI SDKs to work with Llambo as a drop-in replacement.

## Common Use Cases

### Checking Available Models

```bash
# List all available models
curl http://localhost:8080/v1/models | jq '.data[].id'
```

### Verifying Provider Configuration

```bash
# Check which providers are configured and their models
curl http://localhost:8080/v1/models | jq '.data[] | "\(.owned_by): \(.id)"'
```

### Integration with OpenAI SDK

```python
import openai

client = openai.OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="dummy"  # API key configured server-side
)

# List available models through Llambo
models = client.models.list()
for model in models.data:
    print(f"Model: {model.id}, Provider: {model.owned_by}")
```

## Integration Notes

### OpenAI SDK Compatibility
Use the base URL `http://localhost:8080/v1` (note: **no trailing slash**).

```python
import openai

client = openai.OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="dummy"  # API key configured server-side
)

# This will list models from all configured providers
models = client.models.list()
```

### Model Selection in Chat Completions
When making chat completion requests, the `model` parameter is treated as a hint. The actual model used is determined by the provider's configuration. The models list shows which models are available for hinting.

### Provider Health
The models endpoint lists all enabled runtime backends regardless of provider health status. Use the `/health` endpoint to check provider health status, `/providers` for detailed provider information, and `llambo ping --models healthy` for catalog health.
