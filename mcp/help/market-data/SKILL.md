---
name: market-data
description: Resolve instruments and retrieve complete, timestamped market snapshots.
---

# Market data
Resolve instruments before requesting quotes or history. Instrument-master prices are BOD reference data, not live quotes.

- [Resolve and quote instruments](resolve.md)

Use `market` modes `search`, `quote`, `ltp`, `ohlc`, and `historical`. Inspect response metadata instead of assuming completeness.
