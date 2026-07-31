# Contributing Guide

Thank you for your interest in contributing to Llambo! This guide will help you get started with development, understand our coding standards, and learn how to submit contributions effectively.

## Development Setup

### Prerequisites

- **Go 1.25.5+** (matches the repository `go.mod`) - check with `go version`
- **GOPATH/bin in your PATH** (`~/go/bin` on Unix-like systems)
- **Git** for version control
- **LLM provider API keys** (for testing with real backends)

### Getting the Source

```bash
# Clone the repository
git clone https://github.com/dotcommander/llambo
cd llambo

# Install dependencies
go mod download
```

### Building and Running

Llambo follows a specific build pattern to ensure the binary is available in your PATH:

```bash
# Build AND symlink to PATH (CRITICAL)
go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo

# Test the installation
llambo --help
```

**Important**: Always use the `ln -sf` command after building. The user runs `llambo` from PATH, not `./llambo`.

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test ./... -cover

# Run tests for specific package
go test ./providers/...

# Run tests with verbose output
go test ./... -v
```

## Go Conventions and Project Patterns

### Code Organization

```
cmd/           - CLI commands (Cobra-based)
  serve.go     - Gateway server command
  config.go    - Config management
  root.go      - Root command

internal/      - Internal packages (not for external use)
  gateway/     - HTTP server, handlers, job processing

providers/     - Backend integration and management
  openai_provider.go    - OpenAI-compatible provider with failover
  circuit_breaker.go   - Per-backend health tracking
  queue.go              - Parallel job processing
  cost_tracker.go       - Token usage and cost aggregation

docs/          - Documentation
  getting-started/ - Installation and setup guides
  api/          - API reference documentation
  guides/       - Feature guides
  architecture/ - Architecture documentation
```

### Naming Conventions

- **Packages**: Use lowercase, single-word names
- **Files**: Use snake_case for multi-word names (`circuit_breaker.go`)
- **Functions**: Use camelCase with descriptive names
- **Variables**: Use camelCase, be descriptive
- **Interfaces**: Use `er` suffix when appropriate (`Provider`, `Executor`)

### Error Handling

- Use Go's standard `error` type
- Return errors with context: `fmt.Errorf("failed to connect: %w", err)`
- Use `errors.New()` for simple error messages
- Wrap errors from external libraries

### Testing Patterns

- Table-driven tests for multiple test cases
- Test edge cases: empty inputs, boundaries, error conditions
- Use `t.Run()` for subtests
- Mock external dependencies when appropriate
- Test both success and failure paths

Example test pattern:

```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name string
        input string
        want string
        wantErr bool
    }{
        {"valid input", "test", "result", false},
        {"empty input", "", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := FunctionName(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("got = %v, want = %v", got, tt.want)
            }
        })
    }
}
```

### Project-Specific Patterns

1. **Circuit Breaker Pattern**: Each backend has independent health tracking
2. **Key Rotation**: Multiple API keys rotate on HTTP 429 responses
3. **Parallel Processing**: Jobs process concurrently across healthy backends
4. **Failover**: Automatic backend switching on failures
5. **Metadata Tracking**: Include `x_llambo` metadata in responses

### Dependencies

- **OpenAI SDK**: `github.com/openai/openai-go` - Native SDK for OpenAI, OpenRouter, etc.
- **Cobra**: `github.com/spf13/cobra` - CLI framework
- **Conc**: `github.com/sourcegraph/conc` - Concurrency utilities
- **Testify**: `github.com/stretchr/testify` - Testing utilities

Avoid adding dependencies unless absolutely necessary. Prefer standard library solutions.

## Pull Request Process

### Branch Strategy

1. **main**: Stable production-ready code
2. **feature/***: New features or enhancements
3. **fix/***: Bug fixes
4. **docs/***: Documentation updates

### Creating a Pull Request

1. **Fork the repository** (if external contributor)
2. **Create a feature branch**: `git checkout -b feature/my-feature`
3. **Make your changes** following the conventions above
4. **Write tests** for new functionality
5. **Run all tests**: `go test ./...`
6. **Commit your changes** with conventional commit messages
7. **Push to your branch**: `git push origin feature/my-feature`
8. **Create a Pull Request** on GitHub

### Commit Message Format

We follow [Conventional Commits](https://www.conventionalcommits.org/) format:

```
type(scope): description

[optional body]

[optional footer]
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `test`: Adding or updating tests
- `refactor`: Code refactoring (no behavior change)
- `chore`: Maintenance tasks, dependencies, etc.

**Scope**: Package or area affected (e.g., `providers`, `gateway`, `cmd`)

**Examples**:
```
feat(providers): add native Gemini API with client caching
fix(gateway): fix job status and add partial_failure
docs(api): update chat completions documentation
test(providers): add circuit breaker edge case tests
```

### PR Review Checklist

Before submitting, ensure your PR:

- [ ] Follows Go conventions and project patterns
- [ ] Includes tests for new functionality
- [ ] All tests pass (`go test ./...`)
- [ ] Documentation is updated if needed
- [ ] Commit messages follow conventional format
- [ ] No breaking changes (unless explicitly intended)
- [ ] Code is clean and well-commented

### Review Expectations

- **Reviewers**: Will check code quality, tests, and adherence to patterns
- **Feedback**: May request changes or suggest improvements
- **Timeline**: Reviews typically within 1-3 business days
- **Discussion**: Use PR comments for technical discussions

## Development Workflow

### Adding a New Provider

1. Check if provider uses OpenAI-compatible API
2. If yes, it works automatically via config (no code changes needed)
3. If no, implement provider interface in `providers/` package
4. Add tests for the new provider
5. Update documentation

### Adding a New Feature

1. Discuss feature in GitHub issue first
2. Design implementation considering existing architecture
3. Implement in smallest logical units
4. Write comprehensive tests
5. Update relevant documentation

### Bug Fixes

1. Reproduce the bug with a test case
2. Fix the issue
3. Add regression test to prevent recurrence
4. Update documentation if behavior changes

## Code Quality

### Before Submitting

1. **Run linter**: `gofmt -d .` (ensure code is formatted)
2. **Run tests**: `go test ./...` (all tests must pass)
3. **Check coverage**: `go test ./... -cover` (aim for >80% in modified packages)
4. **Verify builds**: `go build -o llambo .` (no compilation errors)

### Common Issues to Avoid

- **Double `/v1` in URLs**: Config `base_url` should NOT include `/v1`
- **Memory leaks**: Use context timeouts, limit request sizes
- **Race conditions**: Use proper synchronization for shared state
- **Error swallowing**: Always handle or propagate errors

## Getting Help

- **Issues**: Use GitHub Issues for bug reports and feature requests
- **Discussions**: GitHub Discussions for questions and ideas
- **Documentation**: Check existing docs in `docs/` directory first

## License

By contributing to Llambo, you agree that your contributions will be licensed under the project's MIT License.
