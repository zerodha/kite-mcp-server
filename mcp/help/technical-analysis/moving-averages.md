---
description: "Calculate 20/50-period moving averages from completed candles and identify crossovers."
---

# Moving-average crossover study
1. Resolve the exact instrument with `market` search and obtain `instrument_token`.
2. Request historical candles with explicit dates, for example `market({"mode":"historical","instrument_token":408065,"interval":"day","from_date":"2026-01-01","to_date":"2026-06-30","continuous":false,"oi":false})`. The token is illustrative: replace it with the resolved token. Choose windows permitted by the historical endpoint; narrow or split rejected ranges instead of inventing candles.
3. Sort chronologically, detect duplicates and gaps, and exclude the current incomplete candle. IST governs date interpretation. Today's quote OHLC is not a historical series.
4. With a computation tool, calculate 20- and 50-period simple moving averages from closing prices, requiring 50 completed candles before the first comparison. A bullish crossing requires previous SMA20 <= SMA50 and current SMA20 > SMA50. Report insufficient data instead of a signal when warm-up is missing.
5. Show the latest completed timestamp, closing price, both averages, and crossing dates. Never use a future close to generate an earlier signal. State assumptions about corporate actions, missing sessions, and continuous futures rollovers.
6. A crossover is an observation, not authorization to place an order. Any execution must follow the separate orders approval workflow.
