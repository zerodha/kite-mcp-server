---
title: Help and Skills
description: Embedded workflow recipes, legacy help navigation, and Skills extension compatibility
---

# Help and Skills

One catalog of Markdown files ships inside the Go binary using `go:embed`.
There is no runtime filesystem access, download, script execution, or additional
Kite authentication step for reading documentation. The MCP endpoint's existing
OAuth protection still applies. Account data and mutations continue through
ordinary tools and their existing authentication and elicitation checks.

## Browse workflows

```text
help({"key":"/"})
  /orders
    /orders/prepare
    /orders/reconcile
  /holdings
    /holdings/concentration
    /holdings/positions
  /market-data
    /market-data/resolve
  /technical-analysis
    /technical-analysis/moving-averages
    /technical-analysis/rsi
  /triggers-and-alerts
    /triggers-and-alerts/audit
  /daily-review
    /daily-review/account-briefing
  /troubleshooting
    /troubleshooting/authentication
    /troubleshooting/data
    /troubleshooting/execution
```

Discovery has three levels: `/` lists categories, a category lists its direct
children, and a recipe returns full workflow steps. Each child has a short
description and a `category` or `recipe` label. Branches show only their summary
and navigation; they do not load descendant instructions. Non-root pages include
a parent key for backtracking. Nested supporting directories automatically gain
navigation pages, without adding synthetic resources to Skills manifests. Omitting `key`, or using an empty string,
selects `/`. A single trailing slash is accepted. Keys are limited to 1024 UTF-8 bytes; oversized keys return a tool error before
lookup or recovery. Paths are case-sensitive;
wildcards, URL encoding, path traversal, and host filesystem paths are not
resolved. Invalid input and unknown keys return `isError: true` with guidance.
Unknown keys include the nearest existing ancestor and its described child keys,
so `/orders/unknown` offers the valid order recipes immediately.

Recipes explain pagination, stale/missing data, partial fills, and uncertainty.
Technical analysis requires candles and a host computation tool: the server
itself has no indicator engine. Instructions never substitute for transaction
approval. Check `tools/list` for deployment-specific exclusions before following
a recipe; documentation cannot enable excluded tools.

## Negotiation and wire contract

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

The existing server deliberately negotiates protocol versions through
`2025-11-25` to preserve its nested elicitation approval flow. This implementation
binds the Skills methods to that existing transport using the official Go SDK
v1.8.0 custom-method API. It does **not** enable the `2026-07-28` base protocol or
its `server/discover`/multi-round-trip execution model. A client must support
Skills over the negotiated older transport; clients that only implement the
newer discovery model should use ordinary help/resources on a compatible client.

This distinction matters because the current extension specification targets
`2026-07-28` and declares capabilities via `server/discover`, while existing
hosts also use the initialization-based binding. We retain the server's protocol
policy instead of advertising a base revision its transaction flow cannot serve.

## Authoring

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

## Research references

The implementation follows the Resources-based
[Skills extension](https://skills.extensions.modelcontextprotocol.io/specification/stable/skills)
and [official extension repository](https://github.com/modelcontextprotocol/ext-skills),
rather than the superseded `skills` capability or `skill://index.json` proposal.
The extension delegates file format to the
[Agent Skills specification](https://agentskills.io/specification).
The [Go SDK custom-method API](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/mcp/server.go)
keeps extension requests inside the SDK's normal transport and middleware chain.
Recipe semantics were checked against the repository's schemas and the official
[Kite orders](https://kite.trade/docs/connect/v3/orders/) and
[historical data](https://kite.trade/docs/connect/v3/historical/) references.
