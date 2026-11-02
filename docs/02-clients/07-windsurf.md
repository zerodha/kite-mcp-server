---
title: Windsurf
description: Set up Kite MCP with Windsurf (Codeium)
---

# Windsurf

Windsurf supports remote MCP servers through its Cascade AI agent.

## Configuration via settings

1. Open Settings (`Cmd+,` / `Ctrl+,`)
2. Navigate to Advanced > Cascade > MCP Servers
3. Click "Add custom server"
4. Set transport to `streamable-http` and URL to `https://mcp.kite.trade/mcp`
5. Connect the server and complete Kite OAuth sign-in when prompted.

## Configuration via file

Edit `~/.codeium/windsurf/mcp_config.json`:

```json
{
    "mcpServers": {
        "kite": {
            "serverUrl": "https://mcp.kite.trade/mcp"
        }
    }
}
```

The remote endpoint field is `serverUrl`, not `url`. If your installation uses a different settings layout, follow its MCP configuration guide.

If the Windsurf version does not support native remote connections, use `mcp-remote` as a fallback (requires [Node.js](https://nodejs.org/)):

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

Restart Windsurf after updating the configuration.

The [official GitHub MCP installation guide for Windsurf](https://github.com/github/github-mcp-server/blob/main/docs/installation-guides/install-windsurf.md) also documents the `serverUrl` field.

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that the answer comes from Kite. If no tool
is called, check that the connection is enabled for this conversation.
