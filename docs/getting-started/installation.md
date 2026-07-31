# Installation Guide

Complete installation guide for Llambo, the high-performance LLM gateway service.

## Prerequisites

Before installing Llambo, ensure you have the following:

- **Go 1.25.5 or newer** (matches the repository `go.mod`)
- **GOPATH/bin in your PATH** (`~/go/bin` on Unix-like systems)
- **LLM provider API keys** (Optional for initial installation, required for configuration)

### Verifying Go Installation

Check your Go version:

```bash
go version
```

You should see output like:
```
go version go1.25.5 darwin/amd64
```

If you need to install or update Go, visit [go.dev/dl](https://go.dev/dl).

## Installation Methods

Llambo can be installed using either `go install` (recommended) or `go build` for local development.

### Method 1: Install via `go install` (Recommended)

This method installs Llambo directly to your Go binary directory:

```bash
go install github.com/dotcommander/llambo@latest
```

Or for a specific version:

```bash
go install github.com/dotcommander/llambo@v1.0.0
```

### Method 2: Build from Source

Clone the repository and build locally:

```bash
# Clone the repository
git clone https://github.com/dotcommander/llambo.git
cd llambo

# Build and symlink to PATH (CRITICAL for local development)
go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo
```

**Important**: The symlink step is critical because the `llambo` command expects to be in your PATH. Without it, you'll need to run `./llambo` from the project directory instead of `llambo`.

### Method 3: Run Without Installation

For testing or one-time use, you can run Llambo directly:

```bash
go run . serve
```

## Verification

After installation, verify Llambo is working:

```bash
# Check version
llambo --version

# Show help
llambo --help

# List available commands
llambo
```

You should see output similar to:

```
Llambo - High-performance LLM gateway

Usage:
  llambo [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  config      Manage provider configuration
  help        Help about any command
  jobs        Job testing and stress testing commands
  models      List providers and configured models
  ping        Ping all configured providers
  prompt      Send a prompt to multiple models for comparison
  providers   List configured providers and manage model catalog
  route       Routing tools
  serve       Start the gateway server

Flags:
  -h, --help   help for llambo

Use "llambo [command] --help" for more information about a command.
```

## Troubleshooting

### "Command not found: llambo"

**Cause**: The binary isn't in your PATH.

**Solution**:

1. **Check installation location**:
   ```bash
   which llambo
   ```

2. **If using `go build`**, ensure you ran the symlink command:
   ```bash
   ln -sf $(pwd)/llambo ~/go/bin/llambo
   ```

3. **Add GOPATH/bin to PATH** (if not already):
   ```bash
   echo 'export PATH=$PATH:~/go/bin' >> ~/.bashrc
   source ~/.bashrc
   ```

### "Go version too old"

**Cause**: Llambo requires Go 1.25.5+.

**Solution**: Update Go to the latest version from [go.dev/dl](https://go.dev/dl).

### Permission errors during symlink

**Cause**: Insufficient permissions for `~/go/bin`.

**Solution**:
```bash
# Create directory if it doesn't exist
mkdir -p ~/go/bin

# Ensure you have write permissions
sudo chown -R $(whoami) ~/go
```

## Next Steps

Now that Llambo is installed, proceed to:

1. **[Configuration Guide](../guides/configuration.md)** - Set up your LLM providers and API keys
2. **[Getting Started](../README.md)** - Run your first request
3. **[API Reference](../api/api.md)** - Learn about available endpoints
4. **[Architecture Guide](../architecture/architecture.md)** - Understand how Llambo works

## Quick Test

Once configured, you can test your installation:

```bash
# Start the server
llambo serve

# In another terminal, check health
curl http://localhost:8080/health
```
