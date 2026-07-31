# Module Documentation Convention (Rust-style)

## Purpose

This document describes the Rust-style `//!` module documentation convention used in Llambo. Each Go file should begin with a self-contained documentation header that explains the module's purpose, usage, and examples.

## Rust-style Module Comments

In Rust, `//!` comments at the top of a module file document the entire module. We adapt this pattern for Go by placing a comprehensive comment block at the top of each `.go` file.

### Basic Structure

```go
//! Module: <package>.<filename>
//!
//! Purpose: <Brief description of what this file/module does>
//!
//! Usage:
//!   <How this module is used, what it provides>
//!
//! Examples:
//!   <Code examples showing typical usage patterns>
//!
//! Dependencies:
//!   - <Other packages/modules this depends on>
//!
//! Key Types/Functions:
//!   - <Type/function>: <Description>
//!
//! Architecture Context:
//!   <Where this module fits in the overall system architecture>
```

### Required Sections

1. **Module** - Identifier following pattern `package.filename`
2. **Purpose** - Single-sentence description of what the module does
3. **Usage** - How to use the module, exported interfaces
4. **Examples** - Code examples showing typical usage (Go code blocks)
5. **Dependencies** - Other packages or modules this depends on
6. **Key Types/Functions** - Important exported types and functions
7. **Architecture Context** - Where this fits in the overall system

## Example Headers

### Command Module Example

```go
//! Module: cmd.serve
//!
//! Purpose: Starts the LLM gateway HTTP server with graceful shutdown
//!
//! Usage:
//!   $ llambo serve [--port 8080] [--host 127.0.0.1]
//!   Starts an HTTP server providing OpenAI-compatible API endpoints
//!
//! Examples:
//!   // Basic server startup
//!   cmd := &cobra.Command{
//!       Use:   "serve",
//!       Short: "Start the LLM gateway server",
//!       RunE:  runServe,
//!   }
//!
//!   // Server lifecycle
//!   server.Start("127.0.0.1:8080")
//!   defer server.Shutdown(ctx)
//!
//! Dependencies:
//!   - internal/gateway: Server implementation
//!   - providers: Backend configuration and clients
//!   - github.com/spf13/cobra: CLI framework
//!
//! Key Types/Functions:
//!   - serveCmd: Cobra command definition
//!   - runServe: Command execution function
//!   - printBanner: Startup information display
//!
//! Architecture Context:
//!   CLI entry point → gateway server → provider abstraction → backend APIs
//!   This is the command-line interface to start the gateway service.
```

### Gateway Module Example

```go
//! Module: gateway.server
//!
//! Purpose: HTTP server implementation for LLM gateway with OpenAI-compatible API
//!
//! Usage:
//!   server := gateway.New(configs)
//!   server.Start("127.0.0.1:8080")
//!   server.Shutdown(ctx)
//!
//! Examples:
//!   // Create server with provider configs
//!   configs := map[string]providers.Config{
//!       "openai": {...},
//!       "openrouter": {...},
//!   }
//!   server, err := gateway.New(configs)
//!
//!   // Handle HTTP requests
//!   mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletion)
//!   mux.HandleFunc("POST /v1/embeddings", s.handleEmbeddings)
//!
//! Dependencies:
//!   - providers: ChatProvider, JobQueue, EmbeddingProvider interfaces
//!   - net/http: HTTP server implementation
//!
//! Key Types/Functions:
//!   - Server: Main HTTP server struct
//!   - New(): Server constructor
//!   - Start(): Start HTTP listener
//!   - Shutdown(): Graceful shutdown
//!   - middleware(): Request logging and CORS
//!
//! Architecture Context:
//!   HTTP layer → request handlers → provider abstraction → backend APIs
//!   This module implements the public API surface and request routing.
```

### Provider Module Example

```go
//! Module: providers.openai_provider
//!
//! Purpose: OpenAI-compatible provider with multi-backend failover and key rotation
//!
//! Usage:
//!   provider, err := providers.NewOpenAI(configs)
//!   result, err := provider.ChatWithInfo(ctx, systemPrompt, userMessage)
//!
//! Examples:
//!   // Create provider with multiple backend configs
//!   configs := map[string]providers.Config{
//!       "openai": {BaseURL: "https://api.openai.com", Model: "gpt-4o"},
//!       "openrouter": {BaseURL: "https://openrouter.ai/api", Model: "claude-3.5-sonnet"},
//!   }
//!   provider, _ := providers.NewOpenAI(configs)
//!
//!   // Use provider for chat completion
//!   result, err := provider.ChatWithInfo(ctx,
//!       "You are a helpful assistant.",
//!       "What is the capital of France?")
//!
//! Dependencies:
//!   - github.com/openai/openai-go: OpenAI Go SDK
//!   - Circuit breaker, key rotation, cost tracking internal modules
//!
//! Key Types/Functions:
//!   - OpenAIProvider: Main provider struct
//!   - NewOpenAI(): Provider constructor
//!   - ChatWithInfo(): Chat completion with metadata
//!   - NormalizeModelName(): Model name normalization
//!
//! Architecture Context:
//!   Provider abstraction → per-backend clients → circuit breaker → OpenAI SDK
//!   This module handles single requests with automatic failover across backends.
```

## Implementation Guidelines

### File Location
- Place at the top of every `.go` file
- Immediately after the `package` declaration (no blank lines)

### Content Guidelines
- **Purpose**: Should be 1-2 sentences, clear and specific
- **Usage**: Focus on API surface, not implementation details
- **Examples**: Show real, compilable code snippets
- **Dependencies**: List only direct dependencies
- **Key Types/Functions**: Focus on exported, public interface
- **Architecture Context**: Explain the module's role in the system

### Language
- Use active voice ("Starts the server" not "The server is started")
- Be concise but complete
- Use Go code examples with proper syntax

## Benefits

1. **Self-documenting files**: Each file explains itself without external docs
2. **Consistency**: Uniform documentation structure across the codebase
3. **Discovery**: Easy to understand module purpose without reading implementation
4. **Onboarding**: New developers can quickly grasp module relationships
5. **Maintenance**: Clear documentation of dependencies and architecture

This convention provides **self-documenting** files with **consistency** across the codebase, making **onboarding** easier for new developers.

## Enforcement

All new .go files must include Rust-style module documentation. Existing files should be updated when modified.

This ensures contributors understand how to document new modules and maintain consistency across the codebase.

## Validation

The `docs/CONTRIBUTING_test.go` file includes tests to verify module documentation compliance.

## Related Documentation

- [Architecture Overview](../architecture.md) - System architecture
- [CLAUDE.md](../../CLAUDE.md) - Project instructions and conventions
- [CONTRIBUTING.md](../CONTRIBUTING.md) - Contributor guidelines