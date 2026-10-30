---
title: Tools
description: Complete reference for all MCP tools and their parameters
---

# Tools

Kite MCP exposes 7 grouped tools using a `mode` parameter and a `help` tool using a `key` path. Clients advertising the Skills extension use native Skills discovery in place of `help`. The AI assistant selects the appropriate tool and mode based on the user's request.

## help

Browse bundled Markdown workflows with `help({"key":"/"})`, then follow child
keys such as `/orders/reconcile`, `/holdings/concentration`, or
`/technical-analysis/moving-averages`. Omitting `key` reads the root. Trailing
slashes are accepted; keys are exact and case-sensitive. Unknown keys produce
an MCP tool error with navigation instructions. This read-only tool requires no
Kite API call and does not authorize transactions.

See [Help and Skills](04-help-and-skills.md) for the catalog structure and Skills
protocol interface.

## Pagination contract

Paginated modes are `portfolio holdings` (`full` and `compact`), `portfolio positions`,
`orders list`, `orders all_trades`, `gtt list`, `market search`, and `market option_chain` (strike rows).

- `from` is an integer from 0 to 2147483647, default 0.
- `limit` is an integer from 1 to 500, default 100. Zero, negative, fractional,
  string, and null values are rejected rather than coerced or treated as unlimited.
- These modes always return `data` and `pagination`, including when pagination
  arguments are omitted. An offset beyond the end returns an empty array and
  `has_more: false`. Continue with `from + returned` while `has_more` is true.
- Search results sort by `id`, then `instrument_token`, before slicing. Pages are
  stable for an unchanged instrument catalog; a catalog refresh can change matches.

```json
{"data": [], "pagination": {"from": 0, "limit": 100, "total": 0, "returned": 0, "has_more": false}}
```

**Response-shape change:** clients previously reading bare arrays from these modes
must now read `data`. Holdings `summary`, profile, margins, and other unpaginated
operations retain their existing response shapes. Market quote/LTP/OHLC now use
an explicit `data`/`meta` envelope as documented below.

## portfolio

Retrieve portfolio and account information.

| Mode | Description |
|------|-------------|
| `profile` | User profile: name, email, user ID, broker, account type, enabled products and exchanges |
| `margins` | Available funds and margins across equity and commodity segments |
| `holdings` | Demat holdings with P&L. Supports `type` (full/summary/compact); full and compact support pagination |
| `positions` | Open intraday and overnight positions. Supports pagination |

**Parameters:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `mode` | string | all | **Required.** Operation mode |
| `type` | string | holdings | Holdings data type: `full` (default), `summary`, `compact` |
| `from` | integer | holdings, positions | Pagination start index (0-based, default: 0; see bounds above) |
| `limit` | integer | holdings, positions | Page size: 1-500, default 100; always includes pagination metadata |

Position rows retain the upstream fields and add `view`: `day` for today's
trading activity and `net` for current exposure including overnight holdings.
Rows are paginated together, with day rows first. The same instrument may occur
in both views; use `net` to assess current exposure and never sum the two views.

---

## orders

Manage orders placed during the current trading day.

Placement, modification, and cancellation use form elicitation when the client
advertises support (including legacy `elicitation: {}`). Clients that do not
advertise form support, including URL-only clients, execute through the normal
tool call without an additional server approval form. All order modes remain
available in `tools/list`. Authentication and order validation still apply.

For capable clients, MCP 2026-07-28 uses per-request capabilities and
`InputRequiredResult` with `inputRequests`, followed by a retry carrying
`inputResponses` and opaque `requestState`. Older protocols use nested
`elicitation/create`.
The approval shows the normalized order parameters, including instrument,
exchange, BUY/SELL, quantity, product, order type, price, and validity. Modification
and cancellation also show the latest order history entry fetched from Kite;
omitted modification fields remain unchanged. Placement defaults validity to DAY.
The response must have `action: "accept"` and boolean `content.confirm: true`.
An `approved=true` tool argument never grants approval.

When elicitation is used, declines, cancellations, missing or invalid
confirmation, disconnects, and the two-minute deadline block execution. An
elicitation failure never falls back to direct execution. Continuations still
validate the original approval even if capabilities are omitted on the retry.
Approval failures use the `Trade approval failed [code]` prefix, distinct from
Kite API failures.

Multi-round-trip approval state expires after two minutes and is bound to the
account session, operation, and normalized transaction details. It is consumed
atomically before execution, so a retry cannot reuse an approval. Server restart
invalidates pending approvals. Each account session can hold up to eight pending
approvals, within a server-wide limit of 1,024. Expired or consumed approvals
release capacity.

Each approval authorizes one API attempt with the parameters shown. The server
does not automatically retry mutations; after an ambiguous API failure, inspect
the order book before initiating another trade. On clients supporting form elicitation, a repeated tool call requires
fresh approval. This is client-mediated consent through a trusted MCP client,
not independent user authentication. GTT and Alerts/ATO are outside this approval
gate.

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
| `quantity` | integer | place, modify | Order quantity in units. Required for place; optional for modify |
| `product` | string | place | `CNC` (delivery), `MIS` (intraday), `NRML` (F&O overnight), `MTF` (margin funding) |
| `order_type` | string | place, modify | `MARKET`, `LIMIT`, `SL` (stop-loss limit), `SL-M` (stop-loss market) |
| `price` | number | place, modify | Limit price. Required for `LIMIT` and `SL` orders |
| `trigger_price` | number | place, modify | Trigger price. Required for `SL` and `SL-M` orders |
| `validity` | string | place, modify | `DAY`, `IOC` (immediate or cancel), `TTL` (time-to-live) |
| `validity_ttl` | integer | place, modify | Life span in minutes. Required when the request sets validity to `TTL` |
| `disclosed_quantity` | integer | place, modify | Quantity disclosed to the market |
| `iceberg_legs` | integer | place | Number of legs for iceberg orders |
| `iceberg_quantity` | integer | place | Quantity per leg for iceberg orders |
| `tag` | string | place | Optional alphanumeric label (max 20 characters) |
| `market_protection` | number | place | Market protection % for MARKET/SL-M. `0`=disabled, `1`-`100`=custom, `-1`=auto |
| `from` | integer | list, all_trades | Pagination start index (0-based) |
| `limit` | integer | list, all_trades | Page size: 1-500, default 100 |

For placement and modification, supplied `quantity`, `validity_ttl`, `iceberg_legs`, and `iceberg_quantity` values must be integers from 1 to 2147483647, inclusive. `disclosed_quantity` accepts integers from 0 to 2147483647, inclusive. Fractions, strings, null, and values outside these ranges are rejected before any Kite API request.

Modification leaves omitted fields unchanged. Setting `validity` to `TTL` requires a positive `validity_ttl` in the same request; the duration is included in the approval details and sent to Kite.

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

**List pagination:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `from` | integer | list | Pagination start index |
| `limit` | integer | list | Page size: 1-500, default 100 |

---

## open_kite_view

Model-visible on Apps-capable clients. Opens a read-only interactive Kite view
with initial data. Required `view`: `holdings`, `positions`, `funds`, `orders`,
`gtt`, `option_chain`, `quote`, `historical`, or `search`.

- `option_chain`: `underlying` (default `NIFTY`), optional `exchange` and `expiry`.
- `quote`: required `instruments` (up to 500 IDs).
- `historical`: required `instrument_token`, `from_date`, `to_date`, `interval`.
- `search`: required `query`, optional `filter_on` (default `id`).
- List views support `from` and `limit` with the normal pagination bounds.

The selector is filtered by server tool exclusions. It cannot place, modify,
cancel, or delete orders/triggers. Use ordinary tools for text-only analysis or
clients without Apps support.

## market

Retrieve market data and search instruments.

| Mode | Description |
|------|-------------|
| `quote` | Full market data snapshot: OHLC, volume, bid/ask depth, OI. Up to 500 instruments |
| `ltp` | Last traded price only (lighter than quote) |
| `ohlc` | Today's open, high, low, close |
| `historical` | Historical OHLC candle data for a single instrument |
| `search` | Search the instrument master list across all exchanges |
| `option_chain` | Indexed option chain by underlying and expiry, paginated by strike |

**Common parameters:**

| Parameter | Type | Modes | Description |
|-----------|------|-------|-------------|
| `mode` | string | all | **Required.** Operation mode |
| `instruments` | string[] | quote, ltp, ohlc | 1-500 non-empty strings in `exchange:tradingsymbol` format, e.g. `["NSE:INFY"]`; duplicate symbols are requested once |

Quote, LTP, and OHLC return a consistent envelope:

```json
{"data": {"NSE:INFY": {"last_price": 1500}}, "meta": {"requested_count": 2, "returned_count": 1, "missing_instruments": ["NSE:UNKNOWN"], "partial": true}}
```

`data` contains only resolved symbols from the request. Counts refer to unique
symbols. `meta.missing_instruments` explicitly lists unresolved symbols; a partial
response retains the successful data. If no symbols resolve, the response has
`isError: true`, empty `data`, and an `error` message advising correction of the
exchange/symbols. Missing symbols are not represented as zero-priced quotes.

Market successes expose identical values in JSON text content and
`structuredContent`, with an advertised output schema covering data/meta envelopes
and historical candles. Historical responses use
`{"data": [<candles>], "meta": {"timezone": "Asia/Kolkata"}}`; clients previously
reading a bare candle array must now read `data`. Object envelopes conform to the
[MCP 2025-11-25 output contract](https://modelcontextprotocol.io/specification/2025-11-25/schema#tool).
LTP and OHLC metadata includes `as_of_available: false` and `timestamp_note`:
these upstream endpoints supply no timestamps. Use `mode=quote` for upstream
`timestamp` and `last_trade_time`; receipt time is not a price as-of timestamp.

**Option-chain parameters** (`mode=option_chain`):

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `underlying` | string | — | Required symbol or index name, e.g. `NIFTY`, `NIFTY 50`, `BANKNIFTY`, `RELIANCE`, `SENSEX` |
| `exchange` | string | `NFO` | `NFO`/`NSE` or `BFO`/`BSE` |
| `expiry` | string | Nearest unexpired date | Optional exact `YYYY-MM-DD` |
| `from`, `limit` | integer | 0, 100 | Strike-row pagination; maximum 500 rows per call |
| `snapshot` | string | — | Use the previous response's `meta.catalog_revision` for subsequent pages; a changed catalog returns an error |

`data` contains canonical underlying information, optional real spot and
same-expiry future instrument records, the selected expiry, all available
unexpired expiries, and sorted `strikes`. Each strike has `strike`, `call`, and
`put`; missing sides are null. The index covers the entire loaded instrument
catalog and includes active derivative contracts only. It is rebuilt atomically
with catalog updates. `loaded_at` identifies that catalog snapshot.

Pagination counts **strikes**, not instruments. `meta.complete` and
`meta.truncated` describe the returned page. Continue while `has_more` is true,
using the selected `data.expiry` and catalog revision from page one. Chain
formation makes no live quote calls; request `mode=quote` separately for contract
IDs and distinguish the catalog's reference prices from current quotes.

```json
{"mode":"option_chain","exchange":"NFO","underlying":"NIFTY","limit":500}
```

**Historical parameters** (mode=historical):

| Parameter | Type | Description |
|-----------|------|-------------|
| `instrument_token` | number | Numeric instrument token. Use search mode to find this |
| `interval` | string | `minute`, `3minute`, `5minute`, `10minute`, `15minute`, `30minute`, `60minute`, `day` |
| `from_date` | string | Start date in IST: `YYYY-MM-DD HH:MM:SS` or `YYYY-MM-DD` (00:00:00); must be no later than `to_date` |
| `to_date` | string | End date in IST: `YYYY-MM-DD HH:MM:SS` or `YYYY-MM-DD` (23:59:59) |
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
| `from` | integer | 0 | Pagination start index |
| `limit` | integer | 100 | Page size: 1-500; always includes pagination metadata |
| `include_live_quote` | boolean | `false` | Enrich returned search results with live quote data such as latest `last_price`, `lower_circuit_limit`, and `upper_circuit_limit` |

---

Search responses include a `meta` block. By default, `instrument_data_source` is `bod_instruments`, which means fields such as circuit limits come from the beginning-of-day instrument master. When `include_live_quote=true`, quotes are fetched only for the returned page.
`live_quote_enriched` is true only if at least one result was enriched. The
`live_quote_fields` list uses the actual output names: `last_price`,
`lower_circuit_limit`, `upper_circuit_limit`, `ohlc`, and `quote_timestamp`.
`missing_live_quotes` lists instruments for which enrichment was unavailable;
those records retain their BOD values and have no invented quote timestamp.
If no quotes resolve, `live_quote_fields` is empty. Enrichment describes the data
source, not a guarantee of freshness: inspect the upstream `quote_timestamp` and
market session when interpreting a returned price.

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
