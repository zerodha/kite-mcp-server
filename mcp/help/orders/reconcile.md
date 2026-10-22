---
description: "Inspect order status, partial fills and rejection reasons, and recover safely after a timeout."
---

# Reconcile an order
1. Call `orders({"mode":"list","from":0,"limit":100})`. Follow `pagination.has_more` using `from + returned` until complete.
2. Match the exact order ID. Call `orders({"mode":"history","order_id":"<order-id>"})` for its status trail and `orders({"mode":"trades","order_id":"<order-id>"})` for actual fills.
3. Report status, filled quantity, pending quantity, and rejection reason when present. A partially filled cancellation leaves the fills intact.
4. After a timeout, inspect the order book and fills before proposing another operation. An absent match is not proof that a request failed; ask the user to verify ambiguous outcomes. Never automatically resubmit.

For a daily reconciliation use `orders({"mode":"all_trades","from":0,"limit":100})`, page fully, and aggregate fills by order ID. Today's order book is not a historical ledger.
