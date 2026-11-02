---
title: Examples
description: Prompts for portfolio, market data, and order workflows
---

# Examples

Start with a connected client and ask for a specific task. Your assistant uses
the tools available on your server. These examples are prompts, not live data.

## Portfolio analysis

> Show my holdings. Rank the top five by current market value and include the total invested amount and P&L.

> Show my holdings' total P&L and today's P&L separately. Include the data receipt time.

> Show my current portfolio allocation by instrument, with a chart if this client supports it.

Portfolio responses are snapshots. The server does not provide a historical
portfolio-value series or sector classifications. Comparisons with a benchmark
need additional data and cash-flow assumptions.

## F&O positions

> Group my net open positions by underlying and product. Keep today's activity separate from net exposure.

> Show the combined P&L for my net NIFTY positions, including quantities and average prices.

> Show my available funds and the margins currently utilised in the equity and commodity segments.

The server does not calculate option Greeks, strategy breakevens, or estimated
margin for a proposed order. Those analyses require additional inputs and a
separate calculation tool.

## Market data

> What is the current price of Infosys? Include today's high and low and the quote timestamp.

> Compare HDFC Bank, ICICI Bank, and Kotak Mahindra Bank using their current quotes and previous closing prices.

> Get daily candles for Reliance Industries for the last three months. State the instrument and date range used.

Quotes are snapshots, not a streaming feed. Missing quotes are not zero prices.

## Orders and triggers

> Show all orders placed today, including any rejections and their reasons.

> Show my GTT triggers and their status. Do not change them.

> Prepare a limit order to buy one share of INFY on NSE using CNC. Ask me for the limit price and any missing details. Do not submit it.

Preparation is separate from submission. The `orders` tool requires approval
in your client or browser before placing, modifying, or cancelling an order,
unless you have explicitly allowed future orders from this client. See [[Order Approvals]].
GTT and Alerts/ATO mutations do not use that approval guard, so ask only for
changes you explicitly intend. Servers may exclude transaction tools entirely.

## Instrument search

> Find the NSE instrument with trading symbol INFY. Include its instrument token and lot size.

> Show the available expiries and option chain for RELIANCE on NFO. Fetch quotes separately if needed.

> Search the instrument master for ETFs with gold in their name. Show the exchange and full trading symbol.

## Technical analysis

> Fetch daily candles for NSE:RELIANCE and calculate a 20-day moving average using a computation tool. State the date range and any missing candles.

The assistant needs its own computation tool for indicators; Kite MCP supplies
the candles. See [Help and Skills](../help-and-skills/) for workflows that explain
data checks and calculation requirements.
