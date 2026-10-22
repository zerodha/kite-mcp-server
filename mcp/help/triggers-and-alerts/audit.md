---
description: "Compare GTTs and alerts with holdings or positions to identify gaps and duplicate protection."
---

# Audit protective triggers
1. Fetch net positions or holdings and all GTT pages. Fetch enabled alerts using `alerts({"mode":"get","uuids":[],"status":"enabled"})`.
2. Match exchange, exact symbol, product, side, and quantities. Distinguish pending triggers from orders already submitted and from completed fills.
3. Compare trigger and limit prices with a timestamped quote. Report expired, duplicate, unmatched, or quantity-mismatched protection as findings; gaps and illiquidity can prevent execution.
4. For two-leg GTTs inspect both legs and their relation to the user's intended exit conditions. Do not assume notification alerts protect an open position.
5. Propose changes with explicit parameters. Do not silently delete duplicates or create replacements. GTT placement/modification/deletion and alert mutations require server-mediated user approval. Reconcile triggered orders through `orders` history/trades.
