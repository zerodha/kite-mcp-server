---
title: Help and Skills
description: Guided workflows for portfolio reviews, market data, orders, and troubleshooting
---

# Help and Skills

Kite MCP includes workflows your assistant can follow for common tasks. They
explain which data to fetch, what to check, and how to handle missing results.

## Use a workflow

Ask your assistant to find the relevant Kite workflow before starting:

> Use Kite MCP's help to review my portfolio concentration. Include all holdings and flag any missing prices.

> Follow the order reconciliation workflow to check today's orders and fills. Do not place or change any orders.

> Read the code mode guidance before analysing my holdings with code.

Your assistant can browse the `help` tool. In clients with native Skills support,
the same workflows may appear as skills instead. You do not need to install them
separately.

## Available workflows

| Topic | What it helps with |
|-------|--------------------|
| Holdings | Concentration, valuation gaps, and current exposure |
| Market data | Finding exact instruments and checking quote completeness |
| Orders | Preparing an order and reconciling status and fills |
| Technical analysis | Moving averages and RSI from historical candles |
| Triggers and alerts | Reviewing GTTs and alerts for gaps or duplicates |
| Daily review | A snapshot of funds, exposure, orders, and missing data |
| Troubleshooting | Sign-in, incomplete data, and uncertain execution |
| Code mode | Reading tool results and combining data without losing errors |

Technical indicators require a client with a computation tool. A workflow can
use only the tools available on your connection. Reading one does not authorise
an order or schedule a recurring task. See [[Order Approvals]] and [[Code Mode]].

## Browse by help key

If you or your assistant need a specific recipe, start with the root:

```json
{"key":"/"}
```

Pass that argument to `help`, then follow the returned topic and recipe keys.
For example, `/orders/reconcile` opens the order reconciliation workflow, and
`/client-integration/code-mode` opens the code mode guidance.

## Contribute a workflow

Browse the [workflow folder on GitHub](https://github.com/zerodha/kite-mcp-server/tree/develop/mcp/help)
to suggest improvements or contribute a recipe through a pull request.
