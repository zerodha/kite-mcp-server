---
title: Claude Code
description: Set up Kite MCP with Claude Code CLI
---

# Claude Code

Claude Code supports remote MCP servers natively over HTTP.

## Setup

```bash
claude mcp add kite --transport http https://mcp.kite.trade/mcp
```

Start Claude Code, run `/mcp`, and select **Authenticate** for `kite` to complete
Kite sign-in in your browser.

Verify the server was added:

```bash
claude mcp list
```

## Scope

By default, the server is added to the current project. To make it available globally:

```bash
claude mcp add kite --transport http --scope user https://mcp.kite.trade/mcp
```

## Removing

```bash
claude mcp remove kite
```

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that the answer comes from Kite. If no tool
is called, check that the connection is enabled for this conversation.

See the [official Claude Code MCP guide](https://code.claude.com/docs/en/mcp)
for authentication and configuration options.
