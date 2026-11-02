---
title: Quick Start
description: Connect an AI assistant to your Zerodha account
---

# Quick Start

Connect your Zerodha account, sign in through Kite, and start with a read-only
request such as checking your holdings or account balances.

## What you need

- A Zerodha account with Kite.
- An AI client that supports remote MCP connections.
- [Node.js](https://nodejs.org/) only if your chosen setup uses `mcp-remote`.

## Connect your client

1. Choose a setup guide: [[ChatGPT Web]], [[Claude Web]], [[Claude Desktop]],
   [[Claude Code]], [[Codex CLI]], [[Pi]], [[Cursor]], [[VS Code]], [[Windsurf]], [[Cline]], or [[JetBrains]].
   See [[Other Clients]] for additional options.
2. Add the hosted Kite MCP endpoint using your client's connector or MCP settings:

```text
https://mcp.kite.trade/mcp
```

3. Choose OAuth sign-in and complete the Kite login flow in your browser.
4. Open a conversation with the connection enabled and try:

> Use Kite MCP to show my holdings and total P&L. Do not place any orders.

Check that the assistant actually calls a Kite tool and uses its returned data.
A plausible answer alone does not confirm that the connection works.

Clients have different configuration formats. Use your client's setup guide
rather than copying JSON from another client. Clients with native Streamable
HTTP support can connect directly; stdio-only setups use `mcp-remote` as a bridge.

## Authentication

Your client opens a browser or shows a sign-in link. First review the consent
page, including the client name and registered return address. Continue only if
you recognise both, then sign in on Kite's own login page. Your Zerodha password
is not shared with the AI assistant.

The client receives a temporary access token after authorisation. Application
sessions last up to seven days, unless the server restarts or you log out. Kite
access is renewed automatically only when Kite issues a refresh token. Otherwise,
you need to sign in again when Kite access expires.

Hosted clients usually register their OAuth return address automatically. You do
not normally need to create a client ID or paste credentials into a configuration
file. If your organisation manages connections, an administrator may need to
make the connector available first.

## Orders and interactive views

Reading data does not require an order approval. When you request order placement,
modification, or cancellation, review the form in your client or follow the
browser approval link. The browser page optionally lets you allow future orders
from this client. See [[Order Approvals]] before enabling that permission.

Supported clients can also show portfolio charts, instrument details, and other
[MCP Apps](../../reference/mcp-apps/). Other clients receive text data.

## If the connection fails

- **Cannot add the server:** check the endpoint and your client's remote MCP
  support. Use the client-specific guide or the stdio bridge where needed.
- **Sign-in expired:** reconnect through your client's OAuth flow.
- **No tools appear:** enable the connection in the current conversation and
  check your organisation's connector settings.
- **Order form unavailable:** your assistant should select browser review
  automatically. Open its link in the browser used to sign in to Kite MCP.

See the [FAQ](../../reference/faq/) for more troubleshooting.
