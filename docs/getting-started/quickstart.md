# Quick Start

Get Llambo up and running in under 5 minutes with this end-to-end tutorial.

## What You'll Do

1. **Install** Llambo from source
2. **Configure** a mock provider (no API key required)
3. **Run** the gateway server
4. **Test** with your first API call

## Step 1: Install Llambo

First, build and install Llambo:

```bash
# Clone the repository
git clone https://github.com/dotcommander/llambo.git
cd llambo

# Build and symlink to PATH (CRITICAL STEP)
go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo
```

**Verify installation:**

```bash
# Check if llambo is available
llambo --help
```

You should see the command help output with available commands.

## Step 2: Create Basic Configuration

Llambo needs a configuration file. Create the default config:

```bash
# Initialize default configuration
llambo config init
```

This creates `~/.config/llambo/config.json` with example providers. To test without API keys, edit the config to enable the local provider:

```bash
# Edit the config file
nano ~/.config/llambo/config.json
```

Find the `lmstudio` provider section and change `"enabled": false` to `"enabled": true`:

```json
{
  "default_provider": "lmstudio",
  "providers": {
    "lmstudio": {
      "provider_type": "openai",
      "base_url": "http://localhost:1234",
      "model": "local-model",
      "max_tokens": 4096,
      "workers": 2,
      "priority": 10,
      "enabled": true,
      "requires_key": false
    }
  }
}
```

**Note**: This config points to LM Studio on localhost:1234. If you don't have LM Studio running, you can:
1. Install and run [LM Studio](https://lmstudio.ai/) locally
2. Or use any local LLM server (Ollama, llama.cpp, etc.)
3. Or skip to Step 4 and use the test endpoint that works without providers

**Verify your config:**

```bash
llambo config show
```

## Step 3: Start the Gateway

Now start the Llambo server:

```bash
# Start the server (default port 8080)
llambo serve
```

You should see output like:
```
Llambo Gateway
Address: http://127.0.0.1:8080
Backends: 1
```

**Keep this terminal running** - the server needs to stay active.

## Step 4: Test with Your First API Call

Open a **new terminal window** and test the health endpoint:

```bash
curl http://localhost:8080/health
```

You should get a JSON response showing provider status:
```json
{
  "status": "healthy",
  "uptime_seconds": 12,
  "backends": {
    "lmstudio": {
      "healthy": true,
      "failures": 0
    }
  }
}
```

### Test a Chat Completion

Now make your first chat completion request:

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "local-model",
    "messages": [
      {"role": "user", "content": "Hello, Llambo!"}
    ],
    "max_tokens": 50
  }'
```

If you have LM Studio or another local LLM server running, you'll get a real response. If not, you'll see an error showing the provider is unavailable.

**Quick test without local LLM:** Use the health endpoint which works even without providers:
```bash
curl http://localhost:8080/health
```

This always returns a successful response, proving the gateway is running.

**Congratulations!** You've successfully installed, configured, and tested Llambo.

## Next Steps

### Add Real Providers

Replace the local provider with real LLM providers in your config:

1. **OpenAI** (requires API key):
   ```json
   {
     "openai": {
       "base_url": "https://api.openai.com",
       "model": "gpt-4o-mini",
       "api_keys": ["sk-your-api-key-here"],
       "workers": 2,
       "priority": 1,
       "enabled": true
     }
   }
   ```

2. **OpenRouter** (requires API key):
   ```json
   {
     "openrouter": {
       "base_url": "https://openrouter.ai/api",
       "model": "anthropic/claude-3-5-sonnet",
       "env_var": "OPENROUTER_API_KEY",
       "workers": 2,
       "priority": 2,
       "enabled": true
     }
   }
   ```

### Explore More Features

- **Model catalog checks**: `llambo providers refresh`, `llambo models catalog`, and `llambo ping --models healthy`
- **Prompt fanout**: `llambo prompt --models free "Reply with exactly OK"` or pipe prompt text on stdin
- **Parallel job processing**: `/v1/jobs` endpoint for batch requests
- **Provider status**: `/providers` endpoint to see all configured backends
- **Cost tracking**: `/stats` endpoint for token usage and costs

## Troubleshooting

### "Command not found: llambo"
Make sure you ran the symlink command: `ln -sf $(pwd)/llambo ~/go/bin/llambo`

### Server won't start
Check that port 8080 isn't already in use: `lsof -i :8080`

### Health check fails
Ensure the server is running in another terminal and wait a moment after startup.

### Local provider returns connection error
The local provider requires a running LLM server like LM Studio or Ollama. Start your local LLM server first, or use cloud providers with API keys.

## What's Next?

- [Configuration Guide](../guides/configuration.md) - Detailed provider setup
- [CLI Guide](../guides/cli.md) - Catalog, ping, prompt, jobs, and routing commands
- [API Reference](../api/api.md) - All available endpoints
- [Architecture Guide](../architecture/architecture.md) - How Llambo works internally
