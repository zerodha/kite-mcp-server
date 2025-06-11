# kite-mcp-server justfile
# Use with https://github.com/casey/just
#
# Note: All build and test commands use CGO_ENABLED=0 for static binaries
# and better cross-platform compatibility
#
# Tests require GOEXPERIMENT=synctest (Go 1.23+) for time-dependent tests

# Configuration variables
BINARY_NAME := "kite-mcp.bin"
GO_FLAGS := "CGO_ENABLED=0"
TEST_FLAGS := "CGO_ENABLED=0 GOEXPERIMENT=synctest"
TEST_RACE_FLAGS := "CGO_ENABLED=1 GOEXPERIMENT=synctest"
LDFLAGS := "-s -w"
MAIN_FILE := "main.go"

# List all available commands
default:
    @just --list

# === Build Commands ===

# Build the project with git-derived version
build: _build-with-git-version

# Build with specific version
build-version VERSION: (_build-with-version VERSION)

# Clean build artifacts
clean:
    rm -f {{BINARY_NAME}}
    rm -f coverage.out coverage.html

# === Run Commands ===

# Run the binary directly (builds first for consistency)
run *ARGS: build
    ./{{BINARY_NAME}} {{ARGS}}

# Run with environment variables from .env file
run-env *ARGS: _check-env-file build
    @set -o allexport && source .env && set +o allexport && ./{{BINARY_NAME}} {{ARGS}}

# Run in development mode (stdio)
run-dev: build
    APP_MODE=stdio ./{{BINARY_NAME}}

# Run in HTTP mode
run-http: build
    APP_MODE=http ./{{BINARY_NAME}}

# Run in SSE mode
run-sse: build
    APP_MODE=sse ./{{BINARY_NAME}}

# Serve documentation (HTTP mode on port 8080)
docs-serve: build
    APP_MODE=http APP_PORT=8080 ./{{BINARY_NAME}}

# === Test Commands ===

# Run all tests
test:
    {{TEST_FLAGS}} go test -v ./...

# Run tests with coverage
test-coverage:
    {{TEST_FLAGS}} go test -cover -v ./...

# Generate HTML coverage report
coverage:
    {{TEST_FLAGS}} go test -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    @echo "Coverage report generated at coverage.html"

# Run tests with race detector
test-race:
    {{TEST_RACE_FLAGS}} go test -race -v ./...

# === Code Quality Commands ===

# Format code
fmt:
    go fmt ./...

# Run linter and checks
lint:
    go fmt ./...
    go vet ./...
    @just _run-golangci-lint

# Run all quality checks
check: fmt lint test

# === Version Commands ===

# Show current git version
version:
    @just _get-git-version

# Build and show version of built binary
version-built:
    just build
    ./{{BINARY_NAME}} --version

# === Environment Commands ===

# Initialize test environment
init-env: _create-env-file

# === Release Commands ===

# Create a new release
release VERSION:
    #!/usr/bin/env bash
    # Strip 'v' prefix if present to avoid double 'v'
    VERSION_CLEAN=$(echo "{{VERSION}}" | sed 's/^v//')
    TAG_NAME="v${VERSION_CLEAN}"
    
    echo "Creating release ${TAG_NAME}..."

    # Create git tag
    git tag -a "${TAG_NAME}" -m "Release ${TAG_NAME}"

    echo "✅ Created release ${TAG_NAME}"
    echo "✅ Created git tag"
    echo ""
    echo "Next steps:"
    echo "1. Review the tag: git show ${TAG_NAME}"
    echo "2. Push the tag: git push --tags"
    echo "3. Build release binary: just build-version ${TAG_NAME}"
    echo "4. Create a GitHub release: gh release create ${TAG_NAME} --title \"${TAG_NAME}\" --generate-notes"

# === Dependency Commands ===

# Update all dependencies
deps-update:
    go get -u ./...
    go mod tidy

# === Private Helper Recipes ===

# Build with git-derived version
_build-with-git-version:
    #!/usr/bin/env bash
    VERSION=$(just _get-git-version)
    BUILDSTR="$(just _get-build-string)"
    {{GO_FLAGS}} go build -o {{BINARY_NAME}} -ldflags="{{LDFLAGS}} -X 'main.MCP_SERVER_VERSION=${VERSION}' -X 'main.buildString=${BUILDSTR}'" {{MAIN_FILE}}
    echo "✅ Built {{BINARY_NAME}} with version ${VERSION}"

# Build with specific version
_build-with-version VERSION:
    #!/usr/bin/env bash
    BUILDSTR="$(just _get-build-string)"
    {{GO_FLAGS}} go build -o {{BINARY_NAME}} -ldflags="{{LDFLAGS}} -X 'main.MCP_SERVER_VERSION={{VERSION}}' -X 'main.buildString=${BUILDSTR}'" {{MAIN_FILE}}
    echo "✅ Built {{BINARY_NAME}} with version {{VERSION}}"

# Get git version
_get-git-version:
    @git describe --tags --dirty --always 2>/dev/null || echo "dev"

# Get build string
_get-build-string:
    @echo "$(date -u '+%Y-%m-%d %H:%M:%S UTC') - $(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"

# Check if .env file exists
_check-env-file:
    @if [ ! -f .env ]; then \
        echo "Error: .env file not found. Run 'just init-env' to create one."; \
        exit 1; \
    fi

# Create .env file from template
_create-env-file:
    @if [ ! -f .env ]; then \
        if [ -f .env.example ]; then \
            echo "Creating .env file from .env.example"; \
            cp .env.example .env; \
            echo "✅ Created .env file. Please update with your actual API key and secret."; \
        else \
            echo "❌ .env.example not found. Cannot create .env file."; \
            exit 1; \
        fi; \
    else \
        echo "⚠️  .env file already exists. Not modifying."; \
    fi

# Run golangci-lint if available
_run-golangci-lint:
    @if command -v golangci-lint >/dev/null 2>&1; then \
        echo "Running golangci-lint..."; \
        golangci-lint run; \
    else \
        echo "golangci-lint not found, skipping lint"; \
    fi
