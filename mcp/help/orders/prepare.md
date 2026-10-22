---
description: "Resolve instruments, check funds and prices, and prepare an order for explicit user approval."
---

# Prepare an order for approval
1. Resolve the user's instrument with `market({"mode":"search","query":"INFY","filter_on":"tradingsymbol","limit":10})`. Confirm exchange, exact symbol, expiry, lot size, and tick size from the returned instrument. Quantity is in units; convert lots using the current lot size.
2. Fetch `market({"mode":"quote","instruments":["NSE:INFY"]})` and inspect `meta.missing_instruments` and timestamps. Call `portfolio({"mode":"margins"})`; funds alone do not guarantee order acceptance. This server has no order-margin estimation tool.
3. Establish side, units, product, order type, price/trigger, validity, and variety from the user's request. Do not invent missing intent or treat illustrative prices as live prices.
4. Only after the user requests execution, call `orders` with `mode=place` and those exact parameters. The server presents form elicitation for explicit approval. Unsupported clients cannot mutate orders. Never add an approval override argument.
5. Reconcile the returned ID with history and trades. For modify/cancel first inspect the current order and preserve its variety. A successful cancellation does not reverse existing fills.

Example preparation payload only (not an instruction to execute):
```json
{"mode":"place","variety":"regular","exchange":"NSE","tradingsymbol":"INFY","transaction_type":"BUY","quantity":1,"product":"CNC","order_type":"LIMIT","price":1500,"validity":"DAY"}
```
