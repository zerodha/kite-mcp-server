---
title: Code Mode
description: Use code-capable assistants to analyse Kite data and handle incomplete results
---

# Code Mode

Code mode lets your assistant use code to fetch, combine, and analyse Kite data.
It can calculate holding weights, compare quotes, or compute indicators from
historical candles before returning a short answer. [[Pi]] includes this feature.

You connect Kite MCP once using your client's setup guide. The assistant uses
that connection for data and the client's code tool for calculations.

## Try a task

> Fetch all my holdings, calculate each holding's share of the portfolio, and show the five largest. Flag missing prices and failed requests. Do not place or change any orders.

> Get daily candles for NSE:RELIANCE and calculate a 20-day moving average from completed candles. Include the date range and latest candle timestamp.

> Compare current quotes for NSE:INFY and NSE:TCS. Include the quote timestamps and report either instrument if its quote is missing.

Kite MCP provides data; your client needs a code or computation tool to perform
the analysis. If a calculation fails, ask the assistant to explain which data or
capability is missing.

## Get a complete answer

Ask the assistant to fetch every page before making portfolio-wide claims,
retain the original timestamps, and identify missing data. Filters and one page
of holdings do not necessarily cover your whole portfolio.

Keep today's positions separate from net exposure. A missing quote is not a
zero price. Instrument-master prices are reference values, so request current
quotes when your analysis needs them.

## Guidance for assistants using code

Read the available tool descriptions and use the connected client's tool names.
Most Kite tools select an operation with `mode`; [[Tools]] lists their inputs.

- Check `isError` and failed calls before interpreting results. A completed
  script does not mean every request succeeded.
- Prefer `structuredContent` for successful tool results. JSON objects retain
  their fields; bare lists and null responses are wrapped as `{ "data": ... }`.
  The original JSON text is retained for clients that need it. Help returns
  `{ "key": ..., "text": ... }` alongside its original Markdown. Check
  `isError` first; errors and sign-in instructions can be plain text.
- Fetch subsequent pages using `pagination.from + pagination.returned` while
  `pagination.has_more` is true. Stop and report an inconsistent empty page.
- Combine independent reads in small parallel batches. Resolve an instrument
  before requesting its historical data.
- Retain missing-instrument metadata and quote timestamps in the final answer.
  State whether the analysis covers the account or only selected rows.
- Never automatically repeat a trade after a timeout or script failure. Check
  orders and fills first; completed actions are not rolled back with the script.

## Order approvals

When a tool returns `status=approval_required`, share its `approval_url` as a
clickable link and stop. No order has been submitted. Do not open the page or
approve it on the user's behalf, and do not repeat the original order call.

After the user completes the browser review, use `orders` with
`mode=approval_status` and the returned `approval_id` to check the outcome.
Inspect both the status and any error in the returned result.

Results that reach the approval step include `approval_method_used` (`native`, `browser`,
`remembered_permission`, or `none`), `approval_outcome`, and `fallback_reason`.
`native_elicitation_attempted` and `native_elicitation_completed` distinguish an
attempted form from a valid response. A decline or cancellation completes the
form without approving the order. `approved` means the authorization passed;
check the submission result and order status separately. An empty fallback reason
means no fallback was selected. A client that fails locally before replying may
return no final tool result.

The user may explicitly allow future orders from this client on the browser
page. Do not check or grant this permission for them. Use `orders` with
`mode=permissions` when the user asks to view or revoke it. If future orders
are allowed, share the returned link. Otherwise, explain that each request
already requires approval. See [[Order Approvals]] for expiry and scope.

## Ask for the bundled guidance

> Read Kite MCP's code mode help before starting this analysis.

Your assistant can read `help({"key":"/client-integration/code-mode"})`.
Ready-to-use recipes cover quote comparisons, holdings weights across pages,
and approval-status checks. See [[Pi]] for example requests. Clients
with native Skills support can use the same guidance through their skill list.
See [[Help and Skills]] for other workflows.
