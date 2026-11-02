---
name: orders
description: Inspect orders, reconcile fills, and prepare explicitly approved order changes.
---

# Order workflows
Use `orders` for today's orders and trades. Start with read operations; an order ID acknowledges receipt, not execution.

- [Reconcile an order](reconcile.md): partial fills, rejection, and uncertain outcomes.
- [Prepare an order](prepare.md): instrument checks and user approval.

Never retry a placement after a timeout without reconciling first. Orders use native or browser approval by default. A pending browser result is not a submitted order: share the link and check approval_status after user review. The user may explicitly allow future orders from this connection in the browser; never grant this permission on their behalf. Reading these instructions grants no transaction approval.

For requests to show orders, inspect order status, or review fills interactively, use `open_kite_view({"view":"orders"})` when available. The view is read-only and offers timeline and fills. For reconciliation requiring exact order IDs or mutations, continue with the existing orders workflow. If the view tool is unavailable, use the ordinary read modes and summarize their results.

To change future order permission, call `orders` with `mode=permissions`. When permission is enabled, share its browser link as **Require approval again**; only the user should submit that page. If permission is disabled, explain that each request already requires approval.
