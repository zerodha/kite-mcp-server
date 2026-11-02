---
description: Integrate code mode tool calls with result parsing, pagination, and client-mediated order approval.
---

# Code mode clients

1. Read the server instructions and deployed tool schemas. Use the client's actual
   tool names and authenticated MCP connection. Kite provides grouped tools with a
   `mode` argument, not a server-side code execution endpoint.
2. Check transport exceptions and `CallToolResult.isError`. A resolved promise
   does not prove success. Prefer `structuredContent` for market results; other
   tools may return JSON text in `content`. Authentication instructions and errors
   can be plain text. Do not parse every text block as JSON or discard metadata.
3. Page list results using `pagination`: start at `from=0`, advance by `returned`,
   and continue while `has_more`. Stop on errors or an unexpected zero-row page
   with `has_more=true`. Option chains paginate strikes; retain the selected
   expiry and catalog revision across pages and fetch live quotes separately.
4. Run independent reads in bounded parallel batches and retain per-call errors.
   Keep dependent calls sequential. Never turn missing quotes into zero prices,
   treat INTL reference values as live quotes, or sum `day` and `net` positions.
5. Select approval from client capabilities. Use `approval_method=browser` on
   the first mutation call for ChatGPT mobile or a client known not to display
   forms. If future_orders_allowed=true, use auto unless the user requests another review. Do not ask the user to request a link. Keep approval with the user.
   Never fabricate confirmation or automatically
   submit a browser form. If `status=approval_required`, share `approval_url` as a
   clickable link without opening it yourself. No order has been submitted.
6. After user review, call `orders` with `mode=approval_status` and its
   `approval_id`. Inspect the returned status and nested result; a successful status lookup is not
   itself a successful order. Do not repeat the
   original order call or poll in an unbounded loop.
7. Browser links expire after two minutes. Honour a decline or cancellation.
   The user may explicitly allow future orders from this connection in the browser.
   Never enable this permission for them. To view or revoke it, call `orders`
   with `mode=permissions`. If future_orders_allowed is true, share the returned
   link for user review; otherwise explain that approval is already required.
8. Do not batch mutations in parallel or automatically retry them. A script
   failure does not roll back completed calls. After an uncertain result, inspect
   orders and fills before requesting a new submission and approval.
9. GTT and Alerts/ATO mutations currently sit outside the `orders` approval guard
   and can create future trades. Do not assume all writes are protected.

In Pi's built-in integration, MCP tools default to `codemode` exposure. Use
`searchTools`, `describeTool`, and `describeNamespace` to discover them. Scripts
receive the complete MCP result including `isError` and `structuredContent`.
Pi can use the browser approval link without native form support. The user must
open it in their sign-in browser; the agent must not approve it. Pi does not
render MCP Apps.

Reference: https://mcp.kite.trade/reference/code-mode/
Order approvals: https://mcp.kite.trade/reference/order-approvals/
Pi setup: https://mcp.kite.trade/clients/pi/
Official Pi references: https://pi.dev/docs/latest/mcp and https://pi.dev/docs/latest/codemode
