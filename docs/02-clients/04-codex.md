---
title: Codex CLI
description: Set up Kite MCP with OpenAI Codex CLI
---

# Codex CLI

Codex CLI connects directly to Kite MCP over Streamable HTTP.

## Setup

Add the server:

```bash
codex mcp add kite --url https://mcp.kite.trade/mcp
```

Sign in to Kite through the browser:

```bash
codex mcp login kite
```

Complete the login, then start a new Codex session. Check configured servers with:

```bash
codex mcp list
```

Inside Codex, use `/mcp` to inspect active servers.

## Manual configuration

Alternatively, add this to `~/.codex/config.toml`, then run the login command above:

```toml
[mcp_servers.kite]
url = "https://mcp.kite.trade/mcp"
```

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that the answer comes from Kite. If no tool
is called, check `/mcp` and complete authentication.

## Order approvals

Review the form before placing, modifying, or cancelling an order. If the form
is unavailable, ask for a browser approval link. See [[Order Approvals]] for
browser review and optional future order permission.

See the [official OpenAI MCP documentation](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)
for configuration details.
