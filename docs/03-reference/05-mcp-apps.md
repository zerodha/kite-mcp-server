---
title: MCP Apps
description: Explore portfolio and market data in interactive views inside your AI client
---

# MCP Apps

Some AI clients can show interactive Kite views alongside their answers. You can
browse your portfolio, inspect an instrument, or explore a price chart without
leaving the conversation. Connect Kite MCP using your [[Quick Start]] guide;
there is no separate app to install.

## Open a view

Ask for the view you want:

> Show my holdings and portfolio allocation in the interactive Kite view.

> Open the instrument finder and search for INFY on NSE.

> Show the NIFTY option chain for the nearest expiry with current quotes.

Your assistant opens the view when your client supports MCP Apps. In clients
without this feature, ask for the same information as a text response.

## Available views

| View | What you can do |
|------|-----------------|
| Holdings | Review investments, total P&L, today's P&L, and allocation by instrument |
| Positions | Inspect current net exposure or today's activity, with P&L charts |
| Funds | Check equity and commodity balances and margin breakdowns |
| Instrument finder | Search by symbol, company name, instrument ID, ISIN, or underlying |
| Instrument details | Inspect quotes, bid/offer depth, historical charts, and source data |
| Option chain | Choose an underlying and expiry, then compare call and put contracts |
| Orders | Read order status, rejection reasons, history, and fills |
| GTT monitor | Review trigger legs, status, and distance from current quotes |

The views are read-only. Placing, modifying, or cancelling an order is a separate
chat request with approval in the client or browser, unless you have allowed
future orders from that client. See [[Order Approvals]].

## Find and inspect an instrument

Select a search field and enter the symbol or name. Use **Trading symbol** for
symbols such as `INFY`, or **Company name** for names such as `Infosys`.
Exchange, type, and expiry filters narrow the results on the current page.

Select an instrument to open its overview, market depth, and chart. Choose a
date range and interval for historical candles. On a narrow screen, use
**Back to list** to return to your results. Chart values are also available in a
table, and keyboard users can inspect candles with Left/Right, Home, and End.

INTL instruments contain reference data only. Current quotes and historical
charts are unavailable for these entries.

## Understand totals and refreshes

- **Holdings totals** cover the account. Allocation charts and row totals cover
  the rows currently shown, which can change with filtering and pagination.
- **Net exposure** and **Today's activity** are separate position views. Do not
  add them together.
- **Previous and Next** change pages. Local filters apply to the current page.
- **Refresh** requests another snapshot. Data does not stream continuously.
- **Refresh prices** adds current quotes for the shown instruments; it does not
  recalculate the portfolio's original P&L or account totals.
- Check quote timestamps and receipt times. Quotes, charts, and portfolio data
  may have been fetched at different times. Missing prices are not zero prices.

## If a view does not appear or update

Check the [official MCP Apps support matrix](https://modelcontextprotocol.io/extensions/client-matrix)
and your client's version. Pi does not render MCP Apps; use its text and code
mode tools instead.

If a view opens but cannot refresh, ask for a fresh result in chat. If a search
returns no match, check the search field, exchange, and spelling. For quote or
chart errors, try the exact exchange and symbol, or a shorter historical date
range. See the [[FAQ]] for connection troubleshooting.
