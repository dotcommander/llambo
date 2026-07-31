set dotenv-load := false

export GOWORK := "off"

binary := "llambo"

default:
    @just --list

# Build repo-local llambo binary.
build:
    go build -o {{binary}} .

# Build and symlink llambo into $HOME/go/bin.
link: build
    ln -sf "$(pwd)/{{binary}}" "$HOME/go/bin/{{binary}}"

# Start the gateway on port 8080.
serve port="8080":
    go run . serve --port {{port}}

# Run all tests.
test:
    go test ./...

# Transform the existing cached external evidence without a network request.
evals:
    go run . evals

# Explicitly refresh external evidence before transforming it.
evals-refresh:
    go run . evals --refresh

# Run gofmt on source.
fmt:
    gofmt -w $(rg --files cmd internal providers -g '*.go')
