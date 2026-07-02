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
5. Restart Windsurf

## Configuration via file

Edit `~/.codeium/windsurf/mcp_config.json`:

```json
{
    "mcpServers": {
        "kite": {
            "type": "streamable-http",
            "url": "https://mcp.kite.trade/mcp"
        }
    }
}
```

If the Windsurf version does not support `streamable-http`, use `mcp-remote` as a fallback (requires [Node.js](https://nodejs.org/)):

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

Restart Windsurf after updating the configuration.
