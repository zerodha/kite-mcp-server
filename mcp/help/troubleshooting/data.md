---
description: Resolve symbols, complete pagination and diagnose rejected historical requests.
---

# Missing or incomplete data

- Unknown symbol: resolve the exact exchange and symbol using `market` search. Treat `meta.missing_instruments` as missing data rather than zero.
- Truncated portfolio/order results: follow `pagination.has_more` with `from + returned`; valid limit is 1–500. If returned=0 with has_more=true, stop and flag the inconsistent response rather than loop forever.
- Historical request rejected: verify token, IST date ordering, interval, and window bounds. Split large windows and deduplicate their boundaries. Do not replace missing candles with invented prices.
