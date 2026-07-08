---
title: Tools
description: Complete reference for all MCP tools and their parameters
---

# Tools

Kite MCP exposes 7 tools. Each tool handles a group of related operations via a `mode` parameter. The AI assistant selects the appropriate tool and mode based on the user's request.

## portfolio

Retrieve portfolio and account information.

| Mode | Description |
|------|-------------|
| `profile` | User profile: name, email, user ID, broker, account type, enabled products and exchanges |
| `margins` | Available funds and margins across equity and commodity segments |
| `holdings` | Demat holdings with P&L. Supports `type` (full/summary/compact) and pagination |
| `positions` | Open intraday and overnight positions. Supports pagination |

**Parameters:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `mode` | string | all | **Required.** Operation mode |
| `type` | string | holdings | Holdings data type: `full` (default), `summary`, `compact` |
| `from` | number | holdings, positions | Pagination start index (0-based, default: 0) |
| `limit` | number | holdings, positions | Maximum items to return. Enables pagination metadata in response |

---

## orders

Manage orders placed during the current trading day.

| Mode | Description |
|------|-------------|
| `list` | All orders today (completed, cancelled, rejected). Supports pagination |
| `history` | Status trail for a specific order showing every state transition |
| `trades` | Fills (executions) for a specific order |
| `all_trades` | All trades across all orders today. Supports pagination |
| `place` | Place a new order on the exchange |
| `modify` | Modify an existing open order |
| `cancel` | Cancel an open order |

**Parameters:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `mode` | string | all | **Required.** Operation mode |
| `order_id` | string | history, trades, modify, cancel | Order ID |
| `variety` | string | place, modify, cancel | Order variety: `regular` (default), `co`, `amo`, `iceberg`, `auction` |
| `exchange` | string | place | Exchange: `NSE` (default), `BSE`, `MCX`, `NFO`, `BFO` |
| `tradingsymbol` | string | place | Trading symbol (e.g. `INFY`, `NIFTY2560118000CE`) |
| `transaction_type` | string | place | `BUY` or `SELL` |
| `quantity` | number | place | Number of shares or lots (default: 1) |
| `product` | string | place | `CNC` (delivery), `MIS` (intraday), `NRML` (F&O overnight), `MTF` (margin funding) |
| `order_type` | string | place, modify | `MARKET`, `LIMIT`, `SL` (stop-loss limit), `SL-M` (stop-loss market) |
| `price` | number | place, modify | Limit price. Required for `LIMIT` and `SL` orders |
| `trigger_price` | number | place, modify | Trigger price. Required for `SL` and `SL-M` orders |
| `validity` | string | place, modify | `DAY`, `IOC` (immediate or cancel), `TTL` (time-to-live) |
| `validity_ttl` | number | place | Life span in minutes. Required when validity is `TTL` |
| `disclosed_quantity` | number | place, modify | Quantity disclosed to the market |
| `iceberg_legs` | number | place | Number of legs for iceberg orders |
| `iceberg_quantity` | number | place | Quantity per leg for iceberg orders |
| `tag` | string | place | Optional alphanumeric label (max 20 characters) |
| `market_protection` | number | place | Market protection % for MARKET/SL-M. `0`=disabled, `1`-`100`=custom, `-1`=auto |
| `from` | number | list, all_trades | Pagination start index (0-based) |
| `limit` | number | list, all_trades | Maximum items to return |

---

## gtt

Manage GTT (Good Till Triggered) orders. GTT orders persist across trading sessions and execute automatically when a price condition is met.

| Mode | Description |
|------|-------------|
| `list` | All GTT orders. Supports pagination |
| `place` | Create a new single-leg or two-leg OCO (one-cancels-other) GTT |
| `modify` | Update an existing GTT |
| `delete` | Remove a GTT |

**Parameters:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `mode` | string | all | **Required.** Operation mode |
| `trigger_id` | number | modify, delete | GTT trigger ID |
| `exchange` | string | place, modify | Exchange: `NSE` (default), `BSE`, `MCX`, `NFO`, `BFO` |
| `tradingsymbol` | string | place, modify | Trading symbol |
| `last_price` | number | place, modify | Current price of the instrument |
| `transaction_type` | string | place, modify | `BUY` or `SELL` |
| `product` | string | place | `CNC`, `NRML`, `MIS`, `MTF` |
| `trigger_type` | string | place, modify | `single` or `two-leg` |

**Single-leg parameters** (when trigger_type=single):

| Parameter | Type | Description |
|-----------|------|-------------|
| `trigger_value` | number | Price at which the order triggers |
| `quantity` | number | Order quantity |
| `limit_price` | number | Limit price for the resulting order |

**Two-leg (OCO) parameters** (when trigger_type=two-leg):

| Parameter | Type | Description |
|-----------|------|-------------|
| `upper_trigger_value` | number | Upper trigger price |
| `upper_quantity` | number | Quantity for the upper leg |
| `upper_limit_price` | number | Limit price for the upper leg |
| `lower_trigger_value` | number | Lower trigger price |
| `lower_quantity` | number | Quantity for the lower leg |
| `lower_limit_price` | number | Limit price for the lower leg |

| `from` | number | list | Pagination start index |
| `limit` | number | list | Maximum items to return |

---

## market

Retrieve market data and search instruments.

| Mode | Description |
|------|-------------|
| `quote` | Full market data snapshot: OHLC, volume, bid/ask depth, OI. Up to 500 instruments |
| `ltp` | Last traded price only (lighter than quote) |
| `ohlc` | Today's open, high, low, close |
| `historical` | Historical OHLC candle data for a single instrument |
| `search` | Search the instrument master list across all exchanges |

**Common parameters:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `mode` | string | all | **Required.** Operation mode |
| `instruments` | string[] | quote, ltp, ohlc | List in `exchange:tradingsymbol` format, e.g. `["NSE:INFY"]` |

**Historical parameters** (mode=historical):

| Parameter | Type | Description |
|-----------|------|-------------|
| `instrument_token` | number | Numeric instrument token. Use search mode to find this |
| `interval` | string | `minute`, `3minute`, `5minute`, `10minute`, `15minute`, `30minute`, `60minute`, `day` |
| `from_date` | string | Start date: `YYYY-MM-DD HH:MM:SS` |
| `to_date` | string | End date: `YYYY-MM-DD HH:MM:SS` |
| `continuous` | boolean | Stitch futures into continuous series (default: false) |
| `oi` | boolean | Include open interest (default: false) |

**Search parameters** (mode=search):

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `search_mode` | string | `search` | `search`, `get_by_id`, `get_by_tradingsymbol`, `get_by_isin`, `get_by_inst_token`, `get_by_exch_token` |
| `verbosity` | string | `compact` | `compact` returns essential fields. `full` returns all details |
| `query` | string | - | Search text. Required for search_mode=search |
| `filter_on` | string | `id` | Field to search: `id`, `name`, `isin`, `tradingsymbol`, `underlying` |
| `id` | string | - | `EXCHANGE:TRADINGSYMBOL`. For search_mode=get_by_id |
| `exchange` | string | - | Exchange code. For get_by_tradingsymbol, get_by_exch_token |
| `tradingsymbol` | string | - | Trading symbol. For get_by_tradingsymbol |
| `isin` | string | - | ISIN. For get_by_isin |
| `inst_token` | number | - | Instrument token. For get_by_inst_token |
| `exch_token` | number | - | Exchange token. For get_by_exch_token |
| `from` | number | 0 | Pagination start index |
| `limit` | number | - | Maximum items to return |
| `include_live_quote` | boolean | `false` | Enrich returned search results with live quote data such as latest `last_price`, `lower_circuit_limit`, and `upper_circuit_limit` |

---

Search responses include a `meta` block. By default, `instrument_data_source` is `bod_instruments`, which means fields such as circuit limits come from the beginning-of-day instrument master. When `include_live_quote=true`, the response also sets `live_quote_enriched=true` and merges selected live quote fields into the returned page of results.

## alerts

Manage Kite price alerts. Alerts trigger when a price condition is met.

| Mode | Description |
|------|-------------|
| `get` | Retrieve alerts. Pass UUIDs for specific alerts, or an empty array for all |
| `create` | Create a new alert (simple or ATO) |
| `modify` | Update an existing alert |
| `delete` | Delete one or more alerts |

**Parameters:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `mode` | string | all | **Required.** Operation mode |
| `uuids` | string[] | all | **Required.** Alert UUIDs. Empty array for get-all. Single UUID for modify. One or more for delete |
| `status` | string | get | Filter: `enabled`, `disabled`, `deleted` |
| `type` | string | get | Filter: `simple`, `ato` |
| `history` | boolean | get | Include trigger history (default: false) |
| `name` | string | create, modify | Alert name |
| `alert_type` | string | create, modify | `simple` or `ato` (alert to order) |
| `lhs_exchange` | string | create, modify | Exchange for the monitored instrument |
| `lhs_tradingsymbol` | string | create, modify | Trading symbol for the monitored instrument |
| `lhs_attribute` | string | create, modify | Attribute to monitor (e.g. `LastTradedPrice`) |
| `operator` | string | create, modify | `<=`, `>=`, `<`, `>`, `==` |
| `rhs_type` | string | create, modify | `constant` (fixed value) or `instrument` (another instrument's attribute) |
| `rhs_constant` | number | create, modify | Target value. Required when rhs_type=constant |
| `rhs_exchange` | string | create, modify | RHS exchange. Required when rhs_type=instrument |
| `rhs_tradingsymbol` | string | create, modify | RHS trading symbol. Required when rhs_type=instrument |
| `rhs_attribute` | string | create, modify | RHS attribute. Required when rhs_type=instrument |
| `basket` | string | create, modify | JSON basket configuration for ATO alerts |

---

## mutual_funds

Retrieve mutual fund data from Coin.

| Mode | Description |
|------|-------------|
| `holdings` | All mutual fund holdings |

**Parameters:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `mode` | string | all | **Required.** Operation mode |

---

## session

Inspect or invalidate the current authenticated session.

| Mode | Description |
|------|-------------|
| `status` | Show the current session state, including whether it is authenticated |
| `logout` | Invalidate the current session so future tool calls require re-authentication |

**Parameters:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `mode` | string | all | **Required.** Operation mode: `status` or `logout` |
