# Development reference

This file covers implementation and contribution details. For connecting a client
and using Kite MCP, see the [user documentation](https://mcp.kite.trade/guide/getting-started/).

## Help catalog and Skills integration


The server declares both `resources` and the extension in its initialize result:

```json
{"capabilities":{"resources":{},"extensions":{"io.modelcontextprotocol/skills":{}}}}
```

A client advertising `capabilities.extensions["io.modelcontextprotocol/skills"]`
as an object in `initialize` receives the Skills interface instead of `help` in
`tools/list`. A stale direct call to `help` returns a tool error directing that
client to Skills. A client that omits the extension retains `help`. The choice
is per negotiated session; one client's settings do not affect another client.
An `experimental` declaration does not activate this behavior.

- `skills/list` returns a deterministic, complete single page. Omit `cursor`;
  an unknown nonempty cursor returns JSON-RPC `-32602`.
- `skills/get({"uri":"skill://orders/SKILL.md"})` returns the same entry as the
  listing. Unknown URIs, supporting-file URIs, and missing URIs return `-32602`.
- Each entry includes `uri`, the complete parsed YAML `frontmatter`, and a
  complete `resources` manifest with raw-byte `size` and `sha256:<lowercase hex>`
  digests, including `SKILL.md` itself.
- List/get results include `resultType: "complete"`, `ttlMs: 300000`, and
  `cacheScope: "public"`. These are static documentation, not account data.
- `resources/read` returns the original Markdown bytes as text, e.g.
  `skill://orders/reconcile.md`. These resources also appear in `resources/list`
  and are readable by generic resource clients. Reading is transport, not skill
  activation or user approval.
- Directory enumeration is optional and is not advertised or implemented.

The server supports `2026-07-28` discovery and multi-round-trip execution,
as well as legacy initialization through `2025-11-25`. Skills capability checks
use each request's metadata for the new protocol and initialized session
capabilities for older clients. A client without the Skills extension uses the
ordinary help tool and resource interfaces.

### Authoring

Browse the [workflow source files on GitHub](https://github.com/zerodha/kite-mcp-server/tree/develop/mcp/help)
to contribute a new workflow or improve an existing one. Submit changes in a pull request.

```text
mcp/help/
  index.md                  # root help page, not an Agent Skill
  orders/
    SKILL.md                # /orders; skill://orders/SKILL.md
    reconcile.md            # /orders/reconcile
    prepare.md              # /orders/prepare
```

Add a topic directory with `SKILL.md`, beginning with YAML frontmatter:

```yaml
---
name: orders
description: Inspect orders, reconcile fills, and prepare explicitly approved order changes.
---
```

Use Agent Skills naming rules; `name` must equal the containing directory name.
Keep entry instructions short and link to sibling recipe files with relative
Markdown links. Give recipe files optional YAML `description` frontmatter for
child-key summaries; without it, the first Markdown heading is used. Skill
branches use their existing frontmatter description. Add nested topic directories with their own `SKILL.md` when
another level is useful. Parent manifests include all nested files. Use only
Markdown in this catalog. The loader validates root presence, unique keys, and
required frontmatter at startup; manifests and navigation are generated, not
maintained in a second registry. Rebuild the Go binary after editing content. Frontmatter accepts LF and CRLF
line endings; parsing normalizes a copy while resource reads and digests preserve
original file bytes. Frontmatter delimiters must occupy a whole line.

`EXCLUDED_TOOLS=help` disables the legacy tool through the ordinary tool filter;
it does not disable public workflow resources or the Skills extension.

Run the repository checks:

```sh
CGO_ENABLED=0 go test -tags=testing ./...
CGO_ENABLED=1 go test -tags=testing -race ./mcp
```

Tests cover tree navigation, invalid keys, links, frontmatter, manifests and
hashes, native list/get/read round trips, simultaneous legacy/Skills sessions,
and an authenticated mock-API order reconciliation workflow over HTTP.

## Order approval implementation


For client and server developers integrating the approval flow.

MCP 2026-07-28 uses per-request capabilities and
`InputRequiredResult` with `inputRequests`, followed by a retry carrying
`inputResponses` and opaque `requestState`. Older protocols use nested
`elicitation/create`.

### Approval contents

The approval shows a readable summary of the normalized order parameters, including instrument,
exchange, BUY/SELL, quantity, product, order type, price, and validity. Modification
and cancellation also show the current order identity, terms, status, and fills
from the latest history entry fetched from Kite;
omitted modification fields remain unchanged. Placement defaults validity to DAY.
The response must have `action: "accept"` and boolean `content.confirm: true`.
An `approved=true` tool argument never grants approval.

### When execution is blocked

Declines, cancellations, missing or invalid
confirmation, disconnects, and the two-minute deadline block execution. An
elicitation failure never falls back to direct execution. Continuations still
validate the original approval even if capabilities are omitted on the retry.
Approval failures use the `Trade approval failed [code]` prefix, distinct from
Kite API failures.

### Approval lifetime and retries

Multi-round-trip approval state expires after two minutes and is bound to the
account session, operation, and normalized transaction details. It is consumed
atomically before execution, so a retry cannot reuse an approval. Server restart
invalidates pending approvals. Each account session can hold up to eight pending
approvals, within a server-wide limit of 1,024. Expired or consumed approvals
release capacity.

Each approval authorizes one API attempt with the parameters shown. The server
does not automatically retry mutations; after an ambiguous API failure, inspect
the order book before initiating another trade. A repeated tool call requires
fresh approval unless the account/client has an explicitly remembered permission. This is client-mediated consent through a trusted MCP client,
not independent user authentication. GTT and Alerts/ATO are outside this approval
gate.

### Browser approval fallback

The application attaches `BrowserApprovals` to the MCP server and serves
`/order-approvals/`. Clients without form elicitation receive an
`approval_required` result with `approval_url`, `approval_id`, and `expires_at`.
`approval_method=browser` requests this path explicitly, including on clients
with native forms. Agents share the link and use `orders approval_status` after
user review rather than repeating the mutation.

Approval requires a browser-only cookie established after Kite login, bound to
the exact application grant and account. It has a distinct JWT purpose and
audience from access tokens. Link possession and bearer tokens do not permit
browser decisions. POST forms require CSRF proof and same-origin requests.

Pending requests retain normalized parameters and a copy of the original tool
arguments. On approval, the server atomically marks the request as submitting,
re-enters the ordinary order handler with fresh authentication, and compares the
normalized details before one API attempt. Current order history is fetched
again for modification and cancellation; changed snapshots fail closed.
Outcomes remain readable by the originating authenticated connection for up to
15 minutes; the oldest completed outcome can be evicted earlier when the bounded
store needs room for a new review. Pending approvals expire after two minutes and are bounded globally
and per grant.

The unchecked browser option can remember permission for placements,
modifications, and cancellations through `orders`. The grant is bound to the
signed OAuth client ID, account, and current application session and expires
with that session. Revocation increments a permission generation so an
in-flight approval cannot re-enable a permission revoked later. Logout,
expiration, and process restart invalidate access. GTT/ATO are outside this gate.
`orders permissions` reports whether permission is enabled and, if so, a
browser-only revocation URL. User-facing documentation lives in Order Approvals.

## MCP Apps implementation


- Resource: `ui://kite/explorer-v1.html`, MIME type `text/html;profile=mcp-app`.
- Tool linkage: `_meta.ui.resourceUri`, with the official SDK helper's compatibility alias `_meta["ui/resourceUri"]`.
- Data: all data tools declare object-root output schemas. `MarshalResponse` retains the original JSON `content` and also returns `structuredContent`: objects keep their fields, while bare lists and null use a `data` wrapper. Raw JSON bytes preserve upstream encoders and integer precision. Help retains Markdown and adds `key`/`text` structured fields. Errors and pending protocol elicitation responses need not contain structured data. Schemas leave upstream account fields open because grouped modes and Kite API responses vary. Apps consume the same structured results; the existing market contract remains intact.
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

### Developing and verifying the view

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

Go tests cover discovery, exclusions, authentication, and response compatibility.
Browser tests exercise the bundled SDK in a sandboxed host, including pagination,
charts, themes, missing data, keyboard navigation, stale responses, and mobile
layouts. They also check that the app makes no direct network requests.
Native client rendering and navigation still need manual verification.

CI rebuilds the UI and rejects an out-of-date committed bundle. It also runs the browser tests. Screenshots are saved under the ignored `mcp/ui/test-artifacts/` directory for local inspection.

### Protocol references

- [Stable MCP Apps specification](https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx)
- [Official Apps quickstart](https://apps.extensions.modelcontextprotocol.io/api/documents/Quickstart.html)
- [Official SDK and examples](https://github.com/modelcontextprotocol/ext-apps)
