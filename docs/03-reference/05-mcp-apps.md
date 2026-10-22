# MCP Apps

Kite's `portfolio` and `market` tools include an inline, read-only explorer in clients supporting the standard `io.modelcontextprotocol/ui` extension. Connect to the same `/mcp` endpoint and authenticate normally; no separate website, browser API key, or UI server is needed.

Ask for holdings, positions, quotes, instrument search, or historical data. An Apps-capable host can render the returned snapshot as a table, with local filtering and column sorting. Paginated responses include Previous and Next controls. Refresh repeats the current request, including its filters and page. Historical data adds a closing-price line chart alongside the complete candle table.

Position views default to **Net exposure**. **Today's activity** is a separate view; they must not be summed. The view selector and text filter apply only to the current page, so another page may contain additional matching positions. The chart uses equal candle spacing rather than elapsed-time spacing. It does not stream ticks; every refresh explicitly requests another snapshot.

## Clients

The [official extension matrix](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/extensions/client-matrix.mdx) lists MCP Apps support in Claude web/Desktop, ChatGPT, Cursor, VS Code GitHub Copilot, Goose and other hosts. Availability depends on the client's version and connector configuration. Listed support is not a claim of live testing in each product.

The server checks each client's `extensions["io.modelcontextprotocol/ui"].mimeTypes` for `text/html;profile=mcp-app`. Supporting clients receive UI metadata on the two tools. Other clients receive the same tools without UI metadata, with the original JSON text responses. The SDK's standard extension capability field is used; no client-name detection is needed. Mixed clients can connect to one server without changing each other's tool definitions.

Existing `EXCLUDED_TOOLS` settings apply as before. Excluding `portfolio` or `market` removes that tool, including its UI entry point. The static UI resource contains no account data and can remain discoverable independently of the tool list. The existing trade approval protocol and trading tools are unchanged.

## Implementation

- Resource: `ui://kite/explorer-v1.html`, MIME type `text/html;profile=mcp-app`.
- Tool linkage: `_meta.ui.resourceUri`, with the official SDK helper's compatibility alias `_meta["ui/resourceUri"]`.
- Data: existing `content` is retained, with object `structuredContent` for rendering. Existing market output schemas and structured responses remain intact.
- Result metadata: `_meta["trade.kite/app"]` identifies the originating tool and arguments. This lets the shared resource refresh the correct request even if a host omits `hostContext.toolInfo`.
- Bridge: the pinned official `@modelcontextprotocol/ext-apps` SDK handles initialization, notifications, tool calls, resizing and teardown.
- Build: bundled HTML is committed and embedded in Go. Ordinary Go builds do not require Node.js, npm, or external asset access.

The view calls only `portfolio` and `market`, through the host's `tools/call` proxy. OAuth and account-session checks remain in the existing server path. It never receives a Kite API secret or token. Both CSP network and resource domain lists are empty, and all upstream strings are rendered as text. There are no external fonts, scripts, analytics, or direct Kite API calls.

MCP Apps is additive to the server's negotiated protocol versions, including
`2026-07-28` and `2025-11-25`. Trade approval uses multi-round-trip input requests
on `2026-07-28` and nested form elicitation on older versions.

## Developing and verifying the view

Use Node.js 20+ (CI uses 22):

```sh
npm --prefix mcp/ui ci
npm --prefix mcp/ui run build
npm --prefix mcp/ui test
cd mcp/ui
npx playwright install chromium
npm run test:browser
```

`just build-app` installs dependencies and rebuilds `mcp/ui/app.html`. `just test-app` runs UI tests after dependencies and Chromium are installed. Set `CHROMIUM_PATH` to use an existing Chromium executable.

Go integration tests cover extension advertisement, resource reads, mixed client tool listings, exclusions, authenticated portfolio reads, and response compatibility. Browser tests run the actual bundled SDK inside a sandboxed iframe and exercise the host handshake, sorting, filtering, pagination, refresh, errors, net/day separation, charts, themes, escaped content and narrow layouts. They assert that the app issues no network requests. This deterministic test host does not replace manual validation in a native Apps client.

CI rebuilds the UI and rejects an out-of-date committed bundle. It also runs the browser tests. Screenshots are saved under the ignored `mcp/ui/test-artifacts/` directory for local inspection.

## Protocol references

- [Stable MCP Apps specification](https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx)
- [Official Apps quickstart](https://apps.extensions.modelcontextprotocol.io/api/documents/Quickstart.html)
- [Official SDK and examples](https://github.com/modelcontextprotocol/ext-apps)
