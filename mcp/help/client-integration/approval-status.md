---
description: Read a browser approval outcome and future-order permission in code mode without repeating a mutation.
---

# Check an approval and future-order permission

Use this after the user has reviewed a browser approval. Read [Code mode clients](code-mode.md)
first and discover the actual tool names. This Pi example assumes the server is named
`kite`. Replace the placeholder with the exact `approval_id` already returned by Kite.
Do not create another order to obtain a new ID.

```javascript
function read(result) {
  if (result.isError) throw new Error(result.content?.find(x => x.type === "text")?.text ?? "Kite request failed");
  if (result.structuredContent) return result.structuredContent;
  for (const item of result.content ?? []) {
    if (item.type !== "text") continue;
    try { const value = JSON.parse(item.text); if (value && typeof value === "object") return value; } catch {}
  }
  throw new Error("Kite returned no JSON data");
}
const approvalId = "REPLACE_WITH_APPROVAL_ID";
if (!approvalId || approvalId === "REPLACE_WITH_APPROVAL_ID") throw new Error("Use the exact ID returned by the pending review");
const state = read(await tools.mcp__kite__orders({ mode: "approval_status", approval_id: approvalId }));
const permission = read(await tools.mcp__kite__orders({ mode: "permissions" }));
let submission = null;
if (state.result) {
  try { submission = read(state.result); }
  catch (error) { submission = { error: error.message }; }
}
text({ approval_id: state.approval_id, status: state.status,
  approval_method_used: state.approval_method_used, approval_outcome: state.approval_outcome,
  fallback_reason: state.fallback_reason, native_elicitation_attempted: state.native_elicitation_attempted,
  native_elicitation_completed: state.native_elicitation_completed, submission, permission });
```

`approval_required` means nothing has been submitted. Share the original review link
and wait for the user. `submitting` means check again later; do not poll indefinitely.
`submitted` means Kite accepted the request, not that it was filled. Use its order ID
with order history and trades to reconcile. A nested error, `failed`, `declined`, or
`expired` is not successful submission. If lookup fails or the outcome is uncertain,
inspect orders and trades; never automatically repeat the mutation.

`future_orders_allowed` describes the current permission separately from this order.
When true, share `permissions_url` only if the user wants to review or revoke it.
The user signs in to Kite and selects **Ask me before every order**. The agent must
not open or submit that page. A status lookup does not grant, revoke, or renew permission.
