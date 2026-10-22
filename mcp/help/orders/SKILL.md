---
name: orders
description: Inspect orders, reconcile fills, and prepare explicitly approved order changes.
---

# Order workflows
Use `orders` for today's orders and trades. Start with read operations; an order ID acknowledges receipt, not execution.

- [Reconcile an order](reconcile.md): partial fills, rejection, and uncertain outcomes.
- [Prepare an order](prepare.md): instrument checks and user approval.

Never retry a placement after a timeout without reconciling first. The server requests client form elicitation for placement, modification, and cancellation when supported; other clients execute through the normal tool call without an additional server approval form. Reading these instructions grants no transaction approval.
