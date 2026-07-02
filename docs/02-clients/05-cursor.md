---
title: Cursor
description: Set up Kite MCP with Cursor IDE
---

# Cursor

Cursor uses stdio-based MCP transport and requires [mcp-remote](https://www.npmjs.com/package/mcp-remote) to connect to remote servers. MCP tools are available in Composer and Agent modes.

## Prerequisites

- [Cursor](https://cursor.sh/)
- [Node.js](https://nodejs.org/)

## Configuration

Open Settings (`Cmd+,` / `Ctrl+,`) > Tools & MCP > Add New MCP Server. Set the transport to `stdio` and the command to:

```
npx mcp-remote https://mcp.kite.trade/mcp
```

Alternatively, create `.cursor/mcp.json` in the project root for a team-shared configuration:

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

Restart Cursor after adding the configuration.
