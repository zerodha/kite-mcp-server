---
description: Assemble funds, exposure, orders and data gaps into a timestamped daily account briefing.
---

# Daily account briefing
1. Call `session({"mode":"status"})`; if authentication has expired, use the client's authentication flow. Never ask for access tokens in chat.
2. Call `portfolio({"mode":"margins"})`, holdings summary, and all pages of positions. Use net rows for current exposure and day rows separately for activity.
3. Fetch all pages of `orders` list and all_trades. Highlight rejected/open orders and reconcile uncertain fills using order history.
4. Check GTTs and enabled alerts when relevant. Price alerts, conditional orders, and actual fills are different states.
5. For held instruments, fetch timestamped quotes in batches of at most 500. Record missing instruments and failed calls.
6. Produce a short report: valuation time, funds, exposure, P&L as reported, pending/rejected orders, and data gaps. Mark partial results explicitly. This is a snapshot, not a tax ledger or a time-weighted return calculation.

The server has no news, fundamentals, corporate-action feed, indicator engine, backtesting engine, or scheduler. Do not claim a recurring briefing was scheduled or external data was retrieved without another tool that actually provides it.
