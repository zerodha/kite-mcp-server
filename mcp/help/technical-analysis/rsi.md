---
description: "Calculate RSI(14) with Wilder smoothing, warm-up checks and explicit edge-case conventions."
---

# Compute and interpret RSI(14)
Resolve a token and retrieve enough completed historical closes using `market` historical mode. Use at least 15 closes and preferably additional warm-up. Check ordering, duplicates, gaps, and candle completion first.

With a computation tool: compute changes between closes; gains=max(change,0), losses=max(-change,0). Seed average gain and loss with the arithmetic mean of the first 14 changes. For each subsequent change use Wilder smoothing: new_average=(13*previous_average+new_value)/14. RSI=100-100/(1+average_gain/average_loss). If only loss is zero use 100; if only gain is zero use 0. For both zero, explicitly choose and disclose a convention, such as 50.

Report interval, latest completed timestamp, warm-up length, smoothing convention, and latest RSI. Values above 70 or below 30 are descriptive thresholds, not guaranteed reversal predictions. Do not fabricate calculations without a computation tool or candles. Corporate actions and contract rollovers can distort indicators.
