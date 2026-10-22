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
- **Streamable HTTP Transport**: Connect through the `/mcp` endpoint
- **Built-in Documentation**: Auto-served documentation at runtime
- **MCP Apps**: Inline portfolio tables and historical charts in compatible clients, with text fallback. See [MCP Apps](docs/03-reference/05-mcp-apps.md).

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
- **For self-hosting**: Go 1.25 or later
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
JWT_SECRET=replace_with_a_random_secret_of_at_least_32_bytes
APP_PORT=8080
APP_HOST=localhost
```

Generate a random signing secret with `openssl rand -hex 32` and use it for
`JWT_SECRET`. For a public deployment, set `OAUTH_ISSUER` to its external HTTPS
base URL and configure the Kite app callback as `<OAUTH_ISSUER>/callback`.

You can also use the provided `justfile` to initialize the config.

```bash
just init-env
```

### Running the Server

```bash
# Export the configuration (.env is not loaded automatically)
set -a
source .env
set +a

# Generate the embedded documentation, build, and run
go generate ./app/
go build -o kite-mcp-server
./kite-mcp-server

# Or generate, build, and load .env using Just
just run-env
```

The server will start and serve documentation at `http://localhost:8080/`

## Client Integration

### Setup Guide

- [Claude Desktop (Hosted Mode)](#claude-desktop-hosted-mode)
- [Claude Desktop (HTTP Mode)](#claude-desktop-http-mode)
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

Start the self-hosted server using the steps above, then add this to your Claude
Desktop configuration (`~/.config/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "kite": {
      "command": "npx",
      "args": ["mcp-remote", "http://localhost:8080/mcp", "--allow-http"]
    }
  }
}
```

Keep the server running separately. Its Kite credentials belong in the server's
environment; the bridge connects through OAuth.

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

- Go 1.25+ (includes `testing/synctest` for timing-dependent tests)

```bash
# Run all tests
just test

# With coverage
just coverage

# With race detector
just test-race

# Direct go command (if you prefer)
CGO_ENABLED=0 go test -tags=testing -v ./...
```

#### Synctest Integration

This project uses Go's `testing/synctest` package for time-dependent tests (session expiry, clock skew tolerance). Timing tests can use `synctest.Test()` for:

- **Fast execution**: Time-dependent tests complete in milliseconds instead of minutes
- **Deterministic timing**: No flaky timing-based test failures
- **Controlled time**: Tests can advance time without actual delays

`testing/synctest` is available without experiment flags in Go 1.25 and later.

## Configuration Options

| Environment Variable | Default     | Description                                                |
| -------------------- | ----------- | ---------------------------------------------------------- |
| `KITE_API_KEY`       | Required    | Your Kite Connect API key                                  |
| `KITE_API_SECRET`    | Required    | Your Kite Connect API secret                               |
| `JWT_SECRET`         | Required    | Secret for signing JWT tokens (min 32 bytes)               |
| `OAUTH_ISSUER`       | _(auto)_    | OAuth issuer URL (defaults to http://host:port)            |
| `APP_PORT`           | `8080`      | HTTP server port                        |
| `APP_HOST`                  | `localhost` | HTTP bind host                        |
| `OAUTH_TOKEN_TTL`           | `168h`       | OAuth access token lifetime (maximum 168h)           |
| `MCP_SESSION_TIMEOUT`       | `24h`       | Streamable HTTP transport idle timeout (Go duration format)  |
| `EXCLUDED_TOOLS`            | _(empty)_   | Comma-separated list of tools to exclude from registration |

The server exposes Streamable HTTP at `/mcp`. Clients requiring a local stdio
connection can use the `mcp-remote` bridge shown above.

### Session lifetime and Kite renewal

Application grants have a fixed seven-day lifetime from login. OAuth access tokens
also default to seven days (`OAUTH_TOKEN_TTL=168h`); a shorter token lifetime can
be configured. Logging out invalidates the grant immediately. Automatic Kite
renewal never extends the seven-day deadline.

Kite access credentials expire independently at the next 06:00 IST cutoff. When
Kite issues a non-empty refresh token, the server retains it privately and renews
expired access credentials on the next tool call. Concurrent calls share the
renewed credentials; requests that place or modify trades are never automatically
replayed. Refresh tokens are available only to approved Kite platforms and grant
long-standing read permissions; renewal does not imply permission to trade.
Without a refresh token, or when Kite rejects it, the user must authenticate again.
Explicit access-token rejection before the estimated cutoff also triggers
renewal for read requests, followed by at most one retry. Mutating requests are
never replayed; their rejected credential is marked stale for the next call.
Temporary renewal failures are shared for five seconds before another attempt.
JWT expiry and the reported `expires_in` are capped by the remaining grant
lifetime. Logged-out, missing, and expired grants receive an HTTP 401 challenge
so MCP clients can initiate authentication again.

Sessions and credentials remain in memory: restarting the server requires users
to log in again, even within seven days. `MCP_SESSION_TIMEOUT` is an independent
transport idle timeout, not the application or Kite credential lifetime.
The server's `/token` endpoint still supports only `authorization_code`; this
renewal is server-to-Kite and does not expose Kite refresh tokens to MCP clients.

### Tool Exclusion

You can exclude specific tools by setting the `EXCLUDED_TOOLS` environment variable with a comma-separated list of tool names. This is useful for creating read-only instances.

**Example:**

```env
EXCLUDED_TOOLS=orders,gtt,alerts
```

The hosted version at `mcp.kite.trade` excludes potentially destructive trading operations for security. For accessing the other operations you can generate your own API keys and run the server locally.

### Open Client Registration

Clients must register through `POST /register` before authorization; no approval is needed. Authorization verifies a signed client ID and requires an exact match against a registered callback. Redirect URIs must use HTTPS; HTTP is allowed for loopback callbacks (`localhost`, `127.0.0.1`, or `::1`). Credentials and fragments are rejected.

DCR accepts at most 10 requests per minute and 100 per hour per IP (IPv6 addresses share a /64 limit), per server instance. Invalid registration attempts count toward these limits. Throttled requests receive HTTP 429 with `Retry-After`. Configure `TRUSTED_PROXY_CIDRS` when running behind a reverse proxy so the limits use the originating client IP. Client IDs contain signed callback hashes, so registrations survive restarts and have no expiration or metadata eviction. Instances must share the same `JWT_SECRET` and `OAUTH_ISSUER`; changing either invalidates existing client IDs and requires registration again.

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

Register first using `POST /register` with `{"redirect_uris":["http://localhost:8080/callback"]}`. Use the returned `client_id` below and during token exchange.

1. **Authorization Request** (with PKCE):
   ```
   GET /authorize?
     response_type=code&
     client_id=<registered-client-id>&
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
     -d "client_id=<registered-client-id>" \
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
