---
description: Diagnose expired sessions, missing approval support and disabled tools.
---

# Authentication and access

- Authentication: use `session({"mode":"status"})`. Reauthenticate through the MCP client's OAuth flow when expired. `session({"mode":"logout"})` invalidates the current session; do not log out merely to diagnose an error.
- Trade approvals: clients without native forms receive a browser link. Choose browser review automatically for ChatGPT mobile or a client known not to display forms by setting `approval_method=browser` on the first mutation call. If future_orders_allowed=true, use auto unless the user requests another review. Do not ask the user to request a link. Native failures reported to the server automatically return browser review when available; explicit decline, cancellation, negative confirmation, and expiry stop the request. Share the link; do not open or approve it yourself. A pending approval has not submitted an order. Open it in the browser used for Kite MCP sign-in. After user review, check `orders` mode=`approval_status` using its `approval_id`. Use `orders` mode=`permissions` to check remembered future order permission. Share its revocation link only when future_orders_allowed is true; otherwise explain that each request already requires approval. See https://mcp.kite.trade/reference/order-approvals/.
- Excluded tools: rely on the actual `tools/list`; a recipe documents capabilities but cannot enable a tool disabled by deployment configuration.
