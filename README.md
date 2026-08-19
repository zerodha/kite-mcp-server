# Kite MCP Server

A Model Context Protocol (MCP) server that provides AI assistants with secure access to the Kite Connect trading API. This server enables AI agents to retrieve market data, manage portfolios, and execute trades through a standardized interface.

## TL;DR for Traders

Want to use AI with your Kite trading account? Just add `https://mcp.kite.trade/mcp` to your AI client configuration. No installation or API keys required - it's hosted and ready to use.

## Features

- **Portfolio Management**: View holdings, positions, margins, and mutual fund investments
- **Order Management**: Place, modify, and cancel orders with full order history
- **GTT Orders**: Good Till Triggered order management
- **Market Data Access**: Real-time quotes, historical data, OHLC data
- **Pagination Support**: Automatic pagination for large datasets (holdings, orders, trades)
- **Comprehensive Coverage**: Implements most Kite Connect API endpoints
- **Multiple Deployment Modes**: stdio, HTTP, SSE, and hybrid mode (production)
- **Built-in Documentation**: Auto-served documentation at runtime

## Quick Start

### Hosted Version (Recommended)

The easiest way to get started is with our hosted version at `mcp.kite.trade`. The primary hosted endpoint is `https://mcp.kite.trade/mcp` - no installation or API keys required on your end.

**Quick setup:** point your MCP client at:

```text
https://mcp.kite.trade/mcp
```

If your client supports project-local HTTP MCP config, you can also add a `.mcp.json` file like this:

```json
{
  "mcpServers": {
    "kite-mcp": {
      "type": "http",
      "url": "https://mcp.kite.trade/mcp"
    }
  }
}
```

Clients without native HTTP transport support can use [mcp-remote](https://github.com/modelcontextprotocol/mcp-remote) as a bridge.

For self-hosting with your own API keys, follow the installation steps below.

### Prerequisites

- **For hosted version (recommended)**: Nothing! Just use `https://mcp.kite.trade/mcp`
- **For self-hosting**: Go 1.21 or later
- **For self-hosting**: Valid Kite Connect API credentials

### Getting started

```bash
git clone https://github.com/zerodha/kite-mcp-server
cd kite-mcp-server
```

### Configuration

Create a `.env` file with your Kite Connect credentials:

```env
KITE_API_KEY=your_api_key
KITE_API_SECRET=your_api_secret
APP_MODE=http
APP_PORT=8080
APP_HOST=localhost
```

You can also use the provided `justfile` to initialize the config.

```bash
just init-env
```

### Running the Server

```bash
# Build and run
go build -o kite-mcp-server
./kite-mcp-server

# Or run directly
go run main.go
```

The server will start and serve a status page at `http://localhost:8080/`

## Client Integration

### Setup Guide

- [Claude Desktop (Hosted Mode)](#claude-desktop-hosted-mode)
- [Claude Desktop (HTTP Mode)](#claude-desktop-http-mode)
- [Claude Desktop (SSE Mode)](#claude-desktop-sse-mode)
- [Claude Desktop (stdio Mode)](#claude-desktop-stdio-mode)
- [Other MCP Clients](#other-mcp-clients)

### Claude Desktop (Hosted Mode)

For the hosted version, add to your Claude Desktop configuration (`~/.config/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "kite": {
      "command": "npx",
      "args": ["mcp-remote", "https://mcp.kite.trade/mcp"]
    }
  }
}
```

### Claude Desktop (HTTP Mode)

Add to your Claude Desktop configuration (`~/.config/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "kite": {
      "command": "npx",
      "args": ["mcp-remote", "http://localhost:8080/mcp", "--allow-http"],
      "env": {
        "APP_MODE": "http",
        "KITE_API_KEY": "your_api_key",
        "KITE_API_SECRET": "your_api_secret"
      }
    }
  }
}
```

### Claude Desktop (SSE Mode)

Add to your Claude Desktop configuration (`~/.config/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "kite": {
      "command": "npx",
      "args": ["mcp-remote", "http://localhost:8080/sse", "--allow-http"],
      "env": {
        "APP_MODE": "sse",
        "KITE_API_KEY": "your_api_key",
        "KITE_API_SECRET": "your_api_secret"
      }
    }
  }
}
```

### Claude Desktop (stdio Mode)

For self-hosted installations, you must first build the binary:

```bash
go build -o kite-mcp-server
```

Then add to your Claude Desktop configuration (`~/.config/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "kite": {
      "command": "/full/path/to/your/kite-mcp-server",
      "env": {
        "APP_MODE": "stdio",
        "KITE_API_KEY": "your_api_key",
        "KITE_API_SECRET": "your_api_secret"
      }
    }
  }
}
```

**Important**: Use the full absolute path to your built binary. For example:

- `/home/username/kite-mcp-server/kite-mcp-server` (Linux)
- `/Users/username/kite-mcp-server/kite-mcp-server` (macOS)
- `C:\Users\username\kite-mcp-server\kite-mcp-server.exe` (Windows)

### Other MCP Clients

For other MCP-compatible clients, use the hosted endpoint `https://mcp.kite.trade/mcp` with [mcp-remote](https://github.com/modelcontextprotocol/mcp-remote) or configure your client to connect directly to the HTTP endpoint.

If your client supports project-local MCP config, the following works well:

```json
{
  "mcpServers": {
    "kite-mcp": {
      "type": "http",
      "url": "https://mcp.kite.trade/mcp"
    }
  }
}
```

## Available Tools

Kite MCP currently exposes 7 grouped tools:

- `portfolio` - Profile, margins, holdings, and positions
- `orders` - Order listing, history, trades, place, modify, cancel
- `gtt` - GTT listing, create, modify, delete
- `market` - Quote, LTP, OHLC, historical data, instrument search
- `alerts` - Alert listing, create, modify, delete
- `mutual_funds` - Mutual fund holdings
- `session` - Session status and logout

See `docs/03-reference/01-tools.md` for the full mode and parameter reference.

## API Coverage

This server implements the majority of Kite Connect API endpoints and also provides additional tools.

## Development

### Development Environment

This project includes a Nix flake for consistent development environments:

```bash
# Enter development shell
nix develop

# Or with direnv
direnv allow
```

### Using Just Commands

Install [Just](https://github.com/casey/just) for convenient development commands:

```bash
just build      # Build the project
just run        # Run the server
just test       # Run tests
just lint       # Format and lint code
just coverage   # Generate coverage report
```

### Running Tests

**Requirements:**

- Go 1.23+ with `GOEXPERIMENT=synctest` (required for timing-dependent tests)

```bash
# Run all tests
just test

# With coverage
just coverage

# With race detector
just test-race

# Direct go command (if you prefer)
CGO_ENABLED=0 GOEXPERIMENT=synctest go test -v ./...
```

#### Synctest Integration

This project requires Go's `synctest` package for time-dependent tests (session expiry, clock skew tolerance). All timing tests use `synctest.Run()` for:

- **Fast execution**: Time-dependent tests complete in milliseconds instead of minutes
- **Deterministic timing**: No flaky timing-based test failures
- **Controlled time**: Tests can advance time without actual delays

The justfile automatically includes `GOEXPERIMENT=synctest` in all test commands.

## Configuration Options

| Environment Variable | Default     | Description                                                |
| -------------------- | ----------- | ---------------------------------------------------------- |
| `KITE_API_KEY`       | Required    | Your Kite Connect API key                                  |
| `KITE_API_SECRET`    | Required    | Your Kite Connect API secret                               |
| `JWT_SECRET`         | Required    | Secret for signing JWT tokens (min 32 bytes)               |
| `OAUTH_ISSUER`       | _(auto)_    | OAuth issuer URL (defaults to http://host:port)            |
| `APP_MODE`           | `http`      | Server mode: `stdio`, `http`, `sse`, or `hybrid`           |
| `APP_PORT`           | `8080`      | Server port (HTTP/SSE/hybrid modes)                        |
| `APP_HOST`                  | `localhost` | Server host (HTTP/SSE/hybrid modes)                        |
| `OAUTH_TOKEN_TTL`           | `24h`       | OAuth access token lifetime (Go duration format)           |
| `MCP_SESSION_TIMEOUT`       | `24h`       | Streamable HTTP MCP session lifetime (Go duration format)  |
| `ALLOWED_REDIRECT_PATTERNS` | `localhost` | Comma-separated redirect URI allowlist patterns            |
| `EXCLUDED_TOOLS`            | _(empty)_   | Comma-separated list of tools to exclude from registration |

**Note:** In production, we use hybrid mode which supports both `/sse` and `/mcp` endpoints, making both HTTP and SSE protocols available for different client needs.

### Tool Exclusion

You can exclude specific tools by setting the `EXCLUDED_TOOLS` environment variable with a comma-separated list of tool names. This is useful for creating read-only instances.

**Example:**

```env
EXCLUDED_TOOLS=orders,gtt,alerts
```

The hosted version at `mcp.kite.trade` excludes potentially destructive trading operations for security. For accessing the other operations you can generate your own API keys and run the server locally.

### Redirect URI Allowlist

By default, the OAuth server only allows loopback redirect URIs for native clients via the special `localhost` allowlist token.

For hosted MCP clients like Claude Web and ChatGPT Web, configure the callback allowlist explicitly:

```env
ALLOWED_REDIRECT_PATTERNS=localhost,https://claude.ai/api/mcp/auth_callback,https://claude.com/api/mcp/auth_callback,https://chatgpt.com/connector_platform_oauth_redirect,prefix:https://chatgpt.com/connector/oauth/
```

Exact callback URIs are preferred. Use prefix patterns only for trusted providers with documented dynamic callback paths, such as ChatGPT connector callbacks under `https://chatgpt.com/connector/oauth/`.

Hosted remote MCP clients generally handle OAuth and dynamic client registration automatically. The user usually only needs to enter the MCP server URL, review the authorize disclaimer interstitial, and complete the Kite login flow in the browser.

## OAuth 2.1 Authentication

The server implements OAuth 2.1 with PKCE for secure authentication. MCP clients can use standard OAuth flows to authenticate users.

### OAuth Endpoints

| Endpoint | Description |
| -------- | ----------- |
| `/.well-known/oauth-authorization-server` | OAuth discovery metadata |
| `/authorize` | Start OAuth authorization flow |
| `/callback` | OAuth callback (handles Kite login redirect) |
| `/token` | Exchange authorization code for JWT token |
| `/mcp` | Protected MCP endpoint (requires Bearer token) |

### OAuth Flow

1. **Authorization Request** (with PKCE):
   ```
   GET /authorize?
     response_type=code&
     client_id=your-client&
     redirect_uri=http://localhost:8080/callback&
     code_challenge=<sha256(code_verifier)>&
     code_challenge_method=S256&
     state=random-state
   ```

2. **User Login**: User is redirected to Kite login page

3. **Callback**: After login, user is redirected back with authorization code

4. **Token Exchange**:
   ```bash
   curl -X POST http://localhost:8080/token \
     -d "grant_type=authorization_code" \
     -d "code=<authorization_code>" \
     -d "redirect_uri=http://localhost:8080/callback" \
     -d "client_id=your-client" \
     -d "code_verifier=<original_verifier>"
   ```

5. **MCP Requests**: Use the JWT token in Authorization header:
   ```bash
   curl -X POST http://localhost:8080/mcp \
     -H "Authorization: Bearer <access_token>" \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","id":1,"method":"initialize",...}'
   ```

### Testing OAuth

A test script is provided to verify the OAuth flow:

```bash
./scripts/test-oauth.sh
```

The script will:
1. Generate PKCE code_challenge
2. Display the authorize URL to open in browser
3. Prompt for the authorization code
4. Exchange for token and test MCP endpoints

### Generate JWT Secret

Generate a secure JWT secret for production:

```bash
openssl rand -hex 32
```

Add to your `.env` file:

```env
JWT_SECRET=<generated-secret>
OAUTH_ISSUER=https://your-domain.com  # Optional, defaults to http://host:port
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Run `just lint` and `just test`
6. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For bugs and general suggestions, please use GitHub Discussions.

For Kite Connect API documentation, visit: https://kite.trade/docs/connect/
