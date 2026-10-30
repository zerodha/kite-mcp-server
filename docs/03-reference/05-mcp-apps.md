---
title: MCP Apps
description: Portfolio and market views inside compatible AI clients
---

# MCP Apps

Kite's `portfolio` and `market` tools include an inline, read-only Kite app in clients supporting the standard `io.modelcontextprotocol/ui` extension. Connect to the same `/mcp` endpoint and authenticate normally; no separate website, browser API key, or UI server is needed.

Ask for holdings, positions, quotes, instrument search, funds, or historical data. The app uses compact instrument rows on mobile, with a table option and local filtering/sorting on desktop. Selecting a holding, position, quote, or search result opens a linked instrument inspector; the list stays alongside it on wide screens. On narrow screens, Back to list returns to the original results.

- **Portfolio overview:** holdings automatically request the unpaginated holdings summary for invested amount, current value, total P&L, and today's P&L. These account-wide totals are fetched separately and do not change with page filters. Pagination reuses the current summary and its original receipt time; an explicit Refresh fetches a new summary once. Row-level P&L totals are explicitly scoped to the shown rows.
- **Instrument inspector:** Overview shows portfolio details and quote statistics, including the day's range, previous close, volume, circuits, and OI where supplied. Depth shows up to five bid/offer levels with quantities and order counts. Details exposes the original data in expandable sections.
- **Linked charts:** Chart requests historical data for the selected instrument token, dates, and interval. Candles include volume when present; incomplete OHLC responses fall back to a closing-price line. Left/Right, Home, and End inspect candle values with the keyboard. The full candle values remain accessible in a table.
- **Positions:** product, direction, quantity, realised/unrealised P&L, and open/closed status remain distinguishable. Local status filters can select open, closed, long, or short positions.
- **Instrument finder:** choose the server search field (instrument ID, company name, trading symbol, ISIN, or underlying), then submit a search, optionally including current quotes, or filter the current page by exchange, type, and expiry. Load contract details requests full verbosity when the original result was compact. The selected search field is retained from chat requests. Searches use the server’s literal matching rules; spaces are not interpreted as separate search terms. Contract lists group by the supplied expiry; absent expiry, strike, or lot-size fields are not inferred from symbol text.
- **Holdings allocation:** a collapsible treemap sizes shown holdings by current value, invested amount, or absolute P&L. Values use the portfolio snapshot; filters and pagination define its coverage.
- **Position P&L chart:** diverging bars show gains and losses for the selected Day or Net view, keeping products separate.
- **Orders:** a paginated order book shows fill progress, rejection reasons, status history, and fills through read-only requests.
- **GTT monitor:** trigger legs and status, with optional current quotes and signed percentage distance from the quote to each trigger.
- **Option chain:** server-indexed NFO/BFO underlying discovery, expiry selection, paired call/put strikes, OI, volume, bid/ask, and LTP. Every strike page for the selected expiry is loaded from one pinned catalog revision; there is no instrument-search cap. Real underlying and same-expiry future references are quoted separately when available. Quotes are batched in groups of at most 500. Mobile defaults to call LTP, strike, and put LTP, with a full-details toggle.
- **Funds:** equity and commodity segments show available margin, cash, collateral, debits, and an expandable breakdown. Disabled segments are labelled explicitly.

Paginated responses include Previous and Next controls. Refresh repeats the current request, including its page and server filters. Retry retains the attempted request even after a bridge error. New main requests cancel auxiliary panel requests, and late panel results cannot replace the main list or a newer instrument selection. Quote requests from the view share a queue with at least 1.1 seconds between starts, including main refreshes and auxiliary quote batches. Waiting requests are cancelled when their view is superseded. This pacing is local to the view; other clients can still share the upstream API quota. Quote and chart panels have separate receipt times; quote timestamps are shown when supplied by the source.

Position views default to **Net exposure**. **Today's activity** is a separate view; they must not be summed. The view selector and text filter apply only to the current page, so another page may contain additional matching positions. Charts use equal candle spacing rather than elapsed-time spacing. It does not stream ticks; every refresh explicitly requests another snapshot. Refresh prices fetches quotes for the current page and adds separate latest-price fields; portfolio P&L and account totals remain tied to their original snapshot.

## Opening a view from chat

Apps-capable clients receive the model-visible, read-only `open_kite_view` tool.
Its view selector supports `holdings`, `positions`, `funds`, `orders`, `gtt`,
`option_chain`, `quote`, `historical`, and `search`. The result includes both the
initial data and the UI resource link, so the client can open the requested
screen directly. For example:

```json
{"view":"orders"}
{"view":"option_chain","underlying":"NIFTY","exchange":"NFO"}
{"view":"search","query":"Infosys","filter_on":"name"}
```

Tool descriptions, server instructions, and workflow help identify when to use
these views. Refresh/pagination aliases and the sidebar launcher remain app-only.
The view tool is omitted for clients without Apps support; ordinary data tools
continue to provide text results. Excluding a source tool removes its views from
the selector and also blocks those views at runtime. Hosts still control UI
rendering; discoverability does not guarantee model selection on every request.

## Clients

The [official extension matrix](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/extensions/client-matrix.mdx) lists MCP Apps support in Claude web/Desktop, ChatGPT, Cursor, VS Code GitHub Copilot, Goose and other hosts. Availability depends on the client's version and connector configuration. Listed support is not a claim of live testing in each product.

The server checks each client's `extensions["io.modelcontextprotocol/ui"].mimeTypes` for `text/html;profile=mcp-app`. Supporting clients receive UI metadata on the two tools. Other clients receive the same tools without UI metadata, with the original JSON text responses. The SDK's standard extension capability field is used; no client-name detection is needed. Mixed clients can connect to one server without changing each other's tool definitions.

Existing `EXCLUDED_TOOLS` settings apply as before. Excluding `portfolio` or `market` removes that tool, including its UI entry point. The static UI resource contains no account data and can remain discoverable independently of the tool list. The existing trade approval protocol and trading tools are unchanged.

## OpenAI plugin entrypoints

Hosts implementing [OpenAI MCP Extensions](https://developers.openai.com/plugins/build/extensions) can open **Kite explorer** from the sidebar or a conversation panel. The app-only `kite_explorer` tool declares `global` and `thread` entrypoints in `_meta["openai/ui"].entrypoints` and accepts `{}`. It loads the first 100 full holdings through the existing authenticated portfolio handler; refresh, pagination, summary, and instrument inspection continue through the standard MCP Apps bridge.

The launcher is advertised only to clients supporting MCP Apps. Apps clients without OpenAI entrypoint support can continue using the inline explorer on `portfolio` and `market`; clients without Apps support retain their original tool list. No client-name detection is used. `EXCLUDED_TOOLS=portfolio` also removes the launcher; `EXCLUDED_TOOLS=kite_explorer` disables only the sidebar/panel integration. Excluding `market` removes the corresponding tool used by the inspector.

The UI resource declares support for inline and fullscreen display, preferring inline for ordinary chat tool calls. Entrypoints open fullscreen as specified by the host. No OpenAI SDK or API key is required for this metadata-only integration. After deploying, refresh the plugin connection and check both entrypoints in a supporting host. Protocol tests cover discovery and launch; native ChatGPT/Codex navigation still requires a manual host check.

## Implementation

- Resource: `ui://kite/explorer-v1.html`, MIME type `text/html;profile=mcp-app`.
- Tool linkage: `_meta.ui.resourceUri`, with the official SDK helper's compatibility alias `_meta["ui/resourceUri"]`.
- Data: existing `content` is retained, with object `structuredContent` for rendering. Existing market output schemas and structured responses remain intact.
- Result metadata: `_meta["trade.kite/app"]` identifies the originating tool and arguments. This lets the shared resource refresh the correct request even if a host omits `hostContext.toolInfo`. Launcher results also carry an `origin` with the launcher tool and its original empty arguments, so the UI can correlate the launch input with the portfolio snapshot without host tool metadata.
- Bridge: the pinned official `@modelcontextprotocol/ext-apps` SDK handles initialization, notifications, tool calls, resizing and teardown.
- Build: bundled HTML is committed and embedded in Go. Ordinary Go builds do not require Node.js, npm, or external asset access.

Prices retain up to four fractional digits so currency ticks and depth levels remain distinguishable; monetary totals use two decimals. Holdings derive invested/current values from settled quantity plus T1 quantity, with the T1 portion shown separately. Source fields in Details remain unmodified.

The view calls `portfolio`, `market`, and the app-only `orders_read` and `gtt_read` tools through the host's `tools/call` proxy. Orders and GTT aliases enforce a server-side read-mode allowlist and respect exclusion of their underlying tools. Panel requests use separate cancellation slots for summaries, quotes, history, and widget reads. They also supply an optional `_app_request_id` correlation argument, declared by the read-tool schemas and returned in the existing result argument metadata. Ordinary chat callers omit this field, so a fresh chat query can repeat a panel query without being mistaken for an echoed notification. Hosts without `serverTools` can still display supplied snapshots and source details; additional requests are disabled with an explanation. OAuth and account-session checks remain in the existing server path. It never receives a Kite API secret or token. Both CSP network and resource domain lists are empty, and all upstream strings are rendered as text. There are no external fonts, scripts, analytics, or direct Kite API calls.

MCP Apps is additive to the server's negotiated protocol versions, including
`2026-07-28` and `2025-11-25`. Trade approval uses multi-round-trip input requests
on `2026-07-28` and nested form elicitation on older versions. The `/mcp`
handler routes requests with `MCP-Protocol-Version: 2026-07-28` or later to
stateless HTTP; older versions retain stateful HTTP sessions.

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

Go integration tests cover extension advertisement, resource reads, mixed client tool listings, exclusions, authenticated portfolio reads, and response compatibility. Browser tests run the actual bundled SDK inside a sandboxed iframe and exercise the host handshake, sorting, filtering, pagination, refresh, errors, net/day separation, charts, themes, escaped content and narrow layouts. Additional explorer tests cover account totals independent of page filters, failed-page retry identity, linked quote/depth/chart panels, manual historical ranges, keyboard candle inspection, echoed notifications, out-of-order quote responses, mobile Back navigation and focus restoration, contract search/filters, position products/status, funds cards, and hosts without `serverTools`. They assert that the app issues no network requests. This deterministic test host does not replace manual validation in a native Apps client.

CI rebuilds the UI and rejects an out-of-date committed bundle. It also runs the browser tests. Screenshots are saved under the ignored `mcp/ui/test-artifacts/` directory for local inspection.

## Protocol references

- [Stable MCP Apps specification](https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx)
- [Official Apps quickstart](https://apps.extensions.modelcontextprotocol.io/api/documents/Quickstart.html)
- [Official SDK and examples](https://github.com/modelcontextprotocol/ext-apps)
