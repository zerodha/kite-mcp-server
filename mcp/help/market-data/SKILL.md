---
name: market-data
description: Resolve instruments and retrieve complete, timestamped market snapshots.
---

# Market data
Resolve instruments before requesting quotes or history. Instrument-master prices are BOD reference data, not live quotes.

- [Resolve and quote instruments](resolve.md)

Use `market` modes `search`, `quote`, `ltp`, `ohlc`, `historical`, and `option_chain`. Inspect response metadata instead of assuming completeness.

For requests to show an option chain, chart, quotes, or instrument finder, prefer `open_kite_view` when that tool is available: `view=option_chain` with `underlying` and optional `exchange`/`expiry`; `view=historical` with instrument token, dates and interval; `view=quote` with instrument IDs; or `view=search` with a query. The host renders the interactive view. Otherwise use the ordinary market tool and summarize its results. `market` also supports `mode=option_chain`; chain contracts are reference data and quotes are fetched separately.
