---
title: Cursor
description: Connect Cursor to Kite MCP over HTTP
---

# Cursor

Cursor supports remote MCP over Streamable HTTP. You do not need a local Node.js
bridge for the direct connection.

## Configuration

Open Cursor's MCP settings to add a server, or create `.cursor/mcp.json` in your
project with:

```json
{
  "mcpServers": {
    "kite": {
      "url": "https://mcp.kite.trade/mcp"
    }
  }
}
```

Save the configuration and connect to the server. Follow the OAuth sign-in flow
through Kite, then enable its tools for your agent conversation.

For a user-wide setup, use Cursor's user MCP configuration instead of the
project file. See the [official Cursor MCP documentation](https://cursor.com/docs/mcp)
for configuration locations and the current feature-support table, including
elicitation support.

## Stdio bridge alternative

If your installation uses stdio, install [Node.js](https://nodejs.org/) and use
this server entry instead:

```json
{
  "mcpServers": {
    "kite": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://mcp.kite.trade/mcp"]
    }
  }
}
```

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Check that a Kite tool returns data. If the server is missing, check the config
file location and the connection's authentication status.
