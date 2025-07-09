# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Model Context Protocol (MCP) server that provides AI assistants with secure access to the Kite Connect trading API. The server enables AI agents to retrieve market data, manage portfolios, and execute trades through a standardized interface.

**The project now includes a Claude Desktop Extension** that packages the MCP server for easy installation and local execution.

**Key Technologies:**
- Go 1.24+ with experimental synctest support
- MCP Go SDK (mark3labs/mcp-go)
- Kite Connect API (zerodha/gokiteconnect/v4)
- Just task runner
- Nix flake for development environment
- Claude Desktop Extension (DXT) specification
- Node.js wrapper for desktop extension integration

## Development Commands

### Build Commands
```bash
# Build with git-derived version
just build

# Build with specific version
just build-version v1.0.0

# Clean build artifacts
just clean
```

### Test Commands
```bash
# Run all tests (requires Go 1.23+ with GOEXPERIMENT=synctest)
just test

# Run tests with coverage report
just coverage

# Run tests with race detector
just test-race
```

### Code Quality
```bash
# Format and lint code
just lint

# Check everything (format, lint, test)
just check
```

### Running the Server
```bash
# Run in different modes
just run-dev     # stdio mode
just run-http    # HTTP mode
just run-sse     # SSE mode
just run-env     # with .env file

# Initialize environment config
just init-env
```

### Desktop Extension Development
```bash
# Sync extension version with git tags
just sync-extension-version

# Build cross-platform binaries for the extension
just build-extension

# Package the extension (requires dxt CLI)
just package-extension

# Test the extension locally
./desktop-extension-claude/test-extension.sh

# Manual commands (if needed)
./desktop-extension-claude/build-binaries.sh  # builds with auto version sync
cd desktop-extension-claude && dxt pack .     # package manually
```

## Architecture

### Main Components

1. **app/** - Core application logic and configuration
   - `app.go` - Main application struct with server lifecycle
   - `metrics/` - Application metrics and monitoring

2. **kc/** - Kite Connect API wrapper and session management
   - `manager.go` - Main Kite Connect API manager
   - `session.go` - User session and authentication handling
   - `session_signing.go` - Cryptographic session validation
   - `instruments/` - Trading instrument search and management
   - `templates/` - HTML templates for status pages

3. **mcp/** - Model Context Protocol implementation
   - `mcp.go` - Main MCP server setup and tool registration
   - `*_tools.go` - Tool implementations grouped by functionality:
     - `setup_tools.go` - Authentication and setup
     - `get_tools.go` - Read-only data retrieval tools
     - `market_tools.go` - Market data and quotes
     - `post_tools.go` - Trading operations (place/modify/cancel orders)
     - `mf_tools.go` - Mutual fund operations

4. **desktop-extension-claude/** - Claude Desktop Extension
   - `manifest.json` - DXT extension manifest with tool definitions
   - `server/index.js` - Node.js proxy wrapper for MCP communication
   - `server/binaries/` - Cross-platform Go binaries (darwin, linux, windows)
   - `build-binaries.sh` - Cross-compilation script for all platforms

### Server Modes

The server supports multiple deployment modes:
- **stdio** - Standard input/output for MCP clients
- **http** - HTTP endpoint at `/mcp`
- **sse** - Server-Sent Events endpoint at `/sse`
- **hybrid** - Both HTTP and SSE endpoints (production mode)

### Desktop Extension Architecture

The Claude Desktop Extension uses a hybrid proxy architecture:
1. **Node.js Proxy** (`server/index.js`) - Handles MCP protocol communication
2. **Local Binary Execution** - Spawns platform-specific Go binary for processing
3. **Fallback to Hosted** - Automatically falls back to `https://mcp.kite.trade/mcp` if local binary fails
4. **Secure Authentication** - Browser-based authentication flow
5. **Session Management** - Persistent session handling with automatic refresh
6. **Request Validation** - Comprehensive input validation and sanitization

### Testing Strategy

Tests use Go's experimental `synctest` package for time-dependent functionality:
- Session expiry testing
- Clock skew tolerance validation
- Deterministic timing without actual delays

All tests must be run with `GOEXPERIMENT=synctest` environment variable.

## Key Configuration

Environment variables:
- `KITE_API_KEY` / `KITE_API_SECRET` - Required Kite Connect credentials
- `APP_MODE` - Server mode (stdio/http/sse/hybrid)
- `APP_PORT` / `APP_HOST` - Server binding configuration
- `EXCLUDED_TOOLS` - Comma-separated list of tools to exclude
- `LOG_LEVEL` - Logging level (debug/info/warn/error)

## Versioning Strategy

The project uses **automated version synchronization** between the MCP server and desktop extension:

- **Single Source of Truth**: Git tags control all component versions
- **Automatic Sync**: `sync-version.sh` updates extension manifest from git tags
- **Version Format**: Git `v0.2.0-dev4` → DXT `0.2.0-dev4`
- **Build Integration**: Version sync runs automatically during extension builds
- **Zero Manual Effort**: No need to manually update version numbers

## Release Automation

The project includes **fully automated DXT release pipeline**:

- **Trigger**: `just release-extension VERSION` creates tag and triggers automation
- **GitHub Actions**: Builds cross-platform binaries and packages DXT automatically
- **Distribution**: Creates GitHub releases with DXT artifacts for immediate download
- **Quality Gates**: Runs tests and validates builds before releasing
- **Monitoring**: Provides real-time build status and completion notifications

### Release Commands
```bash
# Automated release with full pipeline
just release-extension 1.0.0

# Manual release (traditional)
just release 1.0.0
```

## Development Environment

The project includes a Nix flake for consistent development:
```bash
# Enter development shell
nix develop

# Or with direnv
direnv allow
```

## Security Considerations

- Session management uses cryptographic signing for validation
- Tool exclusion mechanism for production deployments
- No API keys required for hosted version at mcp.kite.trade
- Comprehensive input validation across all MCP tools

### Desktop Extension Security

- **Local Execution** - No credentials or data leave the user's machine
- **Secure Authentication** - Browser-based authentication, no API keys stored
- **Request Validation** - All tool calls validated against JSON schemas
- **Input Sanitization** - Protection against injection attacks
- **Session Isolation** - Each extension instance maintains isolated sessions
- **HTTPS Only** - All network communication encrypted in transit
- **Error Handling** - Graceful failure modes with comprehensive logging