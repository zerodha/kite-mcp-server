---
title: Claude Desktop
description: Connect Claude Desktop to Kite MCP
---

# Claude Desktop

Claude Desktop can use a remote connector directly. A local `mcp-remote` bridge
is an alternative for setups that use the desktop developer configuration.

## Remote connector

1. Open Claude's connector settings and add a custom connector.
2. Enter the server URL:

```text
https://mcp.kite.trade/mcp
```

3. Complete OAuth sign-in through Kite, then enable the connection in your chat.

Connector availability may depend on your account or organisation settings.
See [Claude's official connector guide](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp)
for the current settings flow.

## Local bridge alternative

This option requires [Node.js](https://nodejs.org/). Open Claude Desktop's
**Settings > Developer > Edit Config** and add:

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

Save the file, restart Claude Desktop, and follow the browser sign-in flow when
the bridge starts. Check the tool list in a new conversation.

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Expand the tool call and check that it returns account data from Kite.
For other platforms, [[Claude Web]] and [[Claude Code]] are alternatives.
