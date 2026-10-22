---
description: "Separate today's activity from current net exposure and avoid double-counting P&L."
---

# Separate current exposure from today's activity
Call `portfolio({"mode":"positions","from":0,"limit":100})` and fetch all pages. Rows have `view=day` or `view=net`.

Use net rows for current exposure including overnight positions. Day rows describe today's activity. The same contract can occur in both; never sum day and net rows. Preserve exchange, symbol, and product when grouping.

Compare reported realised/unrealised P&L only within the same view and valuation time. Do not combine demat holdings and position quantities without checking overlap. Use `orders` trades for fill reconciliation. Missing rows, stale prices, charges, and cash movements prevent treating this snapshot as audited returns.
