# Embeddings

Generate vector embeddings for text with automatic backend selection.

## Endpoint

```http
POST /v1/embeddings
Content-Type: application/json
```

## Overview

The embeddings endpoint generates vector embeddings for text input using configured backend providers. Embeddings are numerical representations of text that capture semantic meaning, useful for search, clustering, and similarity comparisons.

The endpoint automatically selects the best available embedding provider based on configuration priority and health status.

## Request Body Schema

### EmbeddingRequest

| Field | Type | Required | Description | Constraints |
|-------|------|----------|-------------|-------------|
| `model` | string | No | Model identifier hint | Any string; backend's configured model is actually used |
| `input` | string or array[string] | **Yes** | Text string or array of text strings to embed | Minimum 1 string, maximum limited by request size |

**Note**: The `model` field is treated as a hint. The actual embedding model is determined by the server configuration in `~/.config/llambo/config.json` under the `embed` section.

## Examples

### Single String Request

```bash
curl -X POST http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Hello world"
  }'
```

### Array Request

```bash
curl -X POST http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "input": ["Hello world", "Machine learning is fascinating"]
  }'
```

### Request with Model Hint

```bash
curl -X POST http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "text-embedding-3-small",
    "input": [
      "The quick brown fox jumps over the lazy dog",
      "Artificial intelligence transforms industries"
    ]
  }'
```

### Batch Request

```bash
curl -X POST http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "input": [
      "Document 1: Introduction to quantum computing",
      "Document 2: Principles of machine learning",
      "Document 3: Natural language processing techniques",
      "Document 4: Computer vision applications",
      "Document 5: Reinforcement learning algorithms"
    ]
  }'
```

## Response

### Success Response (HTTP 200)

#### Response Body

```json
{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "index": 0,
      "embedding": [0.0023, -0.0156, 0.0342, -0.0078, 0.0211, ...]
    },
    {
      "object": "embedding",
      "index": 1,
      "embedding": [-0.0089, 0.0234, -0.0112, 0.0456, -0.0034, ...]
    }
  ],
  "model": "text-embedding-3-small",
  "usage": {
    "prompt_tokens": 42,
    "completion_tokens": 0,
    "total_tokens": 42
  }
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `object` | string | Always `"list"` |
| `data` | array[EmbeddingData] | Array of embedding objects |
| `data[].object` | string | Always `"embedding"` |
| `data[].index` | integer | Position of this embedding in the input array |
| `data[].embedding` | array[float32] | Vector embedding values (1536 dimensions by default) |
| `model` | string | Model used for embeddings |
| `usage` | Usage | Token usage information |
| `usage.prompt_tokens` | integer | Estimated tokens in input text |
| `usage.completion_tokens` | integer | Always 0 for embeddings |
| `usage.total_tokens` | integer | Total tokens (same as prompt_tokens) |

**Note**: Token counts are estimated using a fixed multiplier per input string.

### Error Responses

#### Invalid Request (HTTP 400)

```json
{
  "error": {
    "message": "Input array is required",
    "type": "invalid_request",
    "code": "missing_input"
  }
}
```

#### Embedding Provider Not Configured (HTTP 503)

```json
{
  "error": {
    "message": "Embedding provider not configured",
    "type": "not_configured",
    "code": "embedding_unavailable"
  }
}
```

#### Backend Failure (HTTP 502)

```json
{
  "error": {
    "message": "Embedding request failed: rate limit exceeded",
    "type": "upstream_error",
    "code": "embedding_failed"
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

**Note**: Unlike chat completions, embeddings responses do not include `X-Llambo-Provider` or `X-Llambo-Model` headers.

## Constraints and Limits

### Request Size
- Maximum request body size: **10MB**
- Input may be one string or an array of strings
- Minimum input strings: **1 string**
- Maximum input strings: Limited by request size
- Maximum characters per string: Limited by backend provider constraints

### Timeouts
- Handler timeout: **90 seconds**
- Server write timeout: **120 seconds**

### Token Estimation
- Token estimation multiplier: **4 tokens per input string**
- This provides approximate token counts for cost tracking

### Batch Processing
- Inputs are processed in batches of **100 strings** by default
- Batch size configurable in server configuration

## Behavior Notes

### Model Configuration
The actual embedding model is configured server-side in `~/.config/llambo/config.json`:

```json
{
  "embed": {
    "model": "text-embedding-3-small",
    "dimensions": 1536,
    "batch_size": 100
  }
}
```

### Backend Selection
1. Priority given to `openai` or `openai-type` providers
2. Falls back to any enabled provider
3. Uses default OpenAI configuration if no providers configured

### Embedding Dimensions
- Default: **1536 dimensions** (text-embedding-3-small)
- Configurable via `dimensions` in embed config
- Each embedding is a `[]float32` array

### Token Estimation
Since embedding providers typically don't return detailed token counts, Llambo estimates:
- `prompt_tokens = len(input) * 4`
- `completion_tokens = 0`
- `total_tokens = prompt_tokens`

## Common Use Cases

### Text Similarity Search

```bash
curl -X POST http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "input": [
      "How do I reset my password?",
      "Password recovery process",
      "Forgot password help",
      "Account login issues",
      "Software installation guide"
    ]
  }'
```

### Document Clustering

```bash
curl -X POST http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "input": [
      "Machine learning model training requires labeled data",
      "Deep neural networks use multiple layers",
      "Supervised learning uses input-output pairs",
      "Unsupervised learning finds patterns without labels",
      "Reinforcement learning uses reward signals"
    ]
  }'
```

### Semantic Search Indexing

```bash
curl -X POST http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "input": [
      "Product documentation: Getting started guide",
      "Product documentation: API reference",
      "Product documentation: Troubleshooting",
      "Product documentation: Best practices",
      "Product documentation: Release notes"
    ]
  }'
```

## Integration Notes

### OpenAI SDK Compatibility
The endpoint is compatible with the OpenAI SDK embeddings interface.

```python
import openai

client = openai.OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="dummy"  # API key configured server-side
)

response = client.embeddings.create(
    model="text-embedding-3-small",  # Hint only, actual model from config
    input=["Hello world", "Machine learning"]
)

# Access embeddings
for embedding in response.data:
    print(f"Index {embedding.index}: {len(embedding.embedding)} dimensions")
```

### Processing Embedding Results
Embeddings are 1536-dimensional vectors by default (configurable):

```python
# Calculate cosine similarity between two embeddings
import numpy as np

embedding1 = np.array(response.data[0].embedding)
embedding2 = np.array(response.data[1].embedding)

similarity = np.dot(embedding1, embedding2) / (
    np.linalg.norm(embedding1) * np.linalg.norm(embedding2)
)
print(f"Cosine similarity: {similarity:.4f}")
```

### Cost Considerations
- Embedding costs are typically lower than chat completions
- Token counts are estimated for cost tracking
- Batch processing reduces API calls for large inputs
