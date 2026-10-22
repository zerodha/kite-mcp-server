---
name: technical-analysis
description: Compute transparent indicators from historical candles without look-ahead bias.
---

# Technical analysis
The server supplies candles through `market`; it does not calculate indicators or execute strategies. Calculate only when the host has a suitable computation tool, otherwise explain the limitation.

- [Moving-average crossover](moving-averages.md)
- [RSI and data quality](rsi.md)

Use resolved tokens, explicit IST dates, completed candles, and enough warm-up data. Separate observations from proposed trades.
