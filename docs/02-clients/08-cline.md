---
title: Cline
description: Set up Kite MCP with Cline in VS Code
---

# Cline

[Cline](https://github.com/cline/cline) is an agentic VS Code extension with built-in MCP support.

## Configuration

1. Open the Cline sidebar in VS Code
2. Click the MCP Servers icon
3. Add a remote server using **Streamable HTTP**, or add this entry to `cline_mcp_settings.json`:

```json
{
    "mcpServers": {
        "kite": {
            "type": "streamableHttp",
            "url": "https://mcp.kite.trade/mcp"
        }
    }
}
```

If the version does not support remote URLs directly, use `mcp-remote` (requires [Node.js](https://nodejs.org/)):

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

Complete the remote server’s OAuth sign-in through Kite when prompted.

The server status indicator next to the server name should turn green. Use the Restart button if the connection does not initialize.

See the [official Cline MCP guide](https://github.com/cline/cline/blob/main/docs/mcp/mcp-overview.mdx) for transport and configuration details. Set `type` explicitly: omitting it can select legacy SSE.

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that the answer comes from Kite. If no tool
is called, check that the connection is enabled for this conversation.
