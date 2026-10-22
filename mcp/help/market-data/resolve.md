---
description: "Find the exact instrument and retrieve timestamped quotes while checking missing results."
---

# Resolve symbols and obtain a snapshot
1. Search with `market({"mode":"search","query":"INFY","filter_on":"tradingsymbol","limit":10})`. For an exact ID use `{"mode":"search","search_mode":"get_by_id","id":"NSE:INFY"}`.
2. Resolve ambiguity explicitly: exchange, instrument type, expiry, strike, and lot size matter. Never reuse a remembered derivatives token across expiries.
3. Request `market({"mode":"quote","instruments":["NSE:INFY","NSE:SBIN"]})` for depth/OHLC/OI, or `mode=ltp` for a lighter price snapshot. Maximum 500 instruments per call.
4. Read `data` with `meta.missing_instruments`. Missing instruments are not zero prices; an entirely unresolved request is an error. Report quote timestamps and market-closed or stale observations.
5. For search pages advance `from + returned` while `pagination.has_more`. Search is sorted by ID and token; a catalog refresh can change page membership.

`include_live_quote=true` enriches search results; absent enrichment is not proof of a current zero price. Quotes are snapshots, not streaming ticks.
