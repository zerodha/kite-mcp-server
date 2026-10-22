---
description: "Calculate holding weights across complete pages and identify concentration and missing valuations."
---

# Review portfolio concentration
1. Call `portfolio({"mode":"holdings","type":"summary"})` for an overview.
2. Fetch full rows with `portfolio({"mode":"holdings","type":"full","from":0,"limit":100})`; continue while `pagination.has_more`. Do not draw whole-portfolio conclusions from one page.
3. Inspect available settled, unsettled, pledged, and used quantities. State the quantity definition used; do not blindly add overlapping fields. Use per-row quantity and last price only when their semantics are clear.
4. Calculate each holding's value and weight against the sum of comparable values. Report the largest weights and missing prices. Zero total value makes weights undefined.
5. Use ISIN to identify the same security across exchanges before combining it. Sector labels are not provided by this server: request a reliable mapping rather than inventing classifications.
6. Add mutual fund holdings only when requested, using `mutual_funds({"mode":"holdings"})`; disclose potential stock/fund overlap. Present a review, not an automatic rebalance.
