# API Documentation

Start the loopback gateway and check it before sending requests:

```bash
llambo serve
curl http://127.0.0.1:8080/health
```

## Available Documentation

- [API Reference](api.md) - Complete API endpoint documentation
- [Chat Completions](chat-completions.md) - Detailed chat completions documentation
- [Messages](messages.md) - Anthropic-compatible messages endpoint and translation behavior
- [Embeddings](embeddings.md) - Detailed embeddings documentation
- [Operational Endpoints](operations.md) - Health, providers, and stats monitoring
- [Models](models.md) - Model list endpoint behavior
- [Jobs](jobs.md) - Parallel job processing API
- [Error Handling](errors.md) - Error codes and responses

## Base URLs

- Development: `http://localhost:8080`
- Production: Configured based on your deployment

## Quick Reference

| Endpoint | Method | Description |
|----------|--------|-------------|
| [`/v1/chat/completions`](chat-completions.md) | POST | OpenAI-compatible chat completion |
| [`/v1/messages`](messages.md) | POST | Anthropic-compatible messages via translation layer |
| [`/v1/embeddings`](embeddings.md) | POST | Generate embeddings |
| [`/v1/models`](models.md) | GET | List enabled configured models |
| [`/v1/jobs`](api.md#jobs) | POST | Submit batch jobs for parallel processing |
| [`/v1/jobs/{id}`](api.md#jobs) | GET | Get job status and results |
| [`/v1/jobs/{id}/cancel`](api.md#jobs) | POST | Cancel running job |
| [`/health`](operations.md#health-check) | GET | Health check with backend status |
| [`/providers`](operations.md#providers) | GET | List configured providers |
| [`/stats`](operations.md#stats) | GET | Token usage and cost statistics |

## Response Headers

Chat completion responses include provider transparency headers:

| Header | Description |
|--------|-------------|
| `X-Llambo-Provider` | Backend that served the request |
| `X-Llambo-Model` | Model used for completion |

## Gateway access

The server binds to loopback by default. A non-loopback host requires
`llambo serve --allow-remote`. Configure `gateway.auth_token_env` before remote
exposure; when set, every endpoint requires `Authorization: Bearer <token>`.
Set `gateway.allowed_origins` to exact browser origins when CORS is needed.
