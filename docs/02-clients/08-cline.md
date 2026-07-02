---
title: Cline
description: Set up Kite MCP with Cline in VS Code
---

# Cline

[Cline](https://github.com/cline/cline) is an agentic VS Code extension with built-in MCP support.

## Configuration

1. Open the Cline sidebar in VS Code
2. Click the MCP Servers icon
3. Click "Configure" and add to `cline_mcp_settings.json`:

```json
{
    "mcpServers": {
        "kite": {
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
            "args": ["mcp-remote", "https://mcp.kite.trade/mcp"]
        }
    }
}
```

The server status indicator next to the server name should turn green. Use the Restart button if the connection does not initialize.
