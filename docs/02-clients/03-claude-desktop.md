---
title: Claude Desktop
description: Set up Kite MCP with Claude Desktop
---

# Claude Desktop

Claude Desktop uses stdio-based MCP transport and requires [mcp-remote](https://www.npmjs.com/package/mcp-remote) to connect to remote servers.

## Prerequisites

- [Claude Desktop](https://claude.ai/download)
- [Node.js](https://nodejs.org/)

## Configuration

1. Open Claude Desktop > Settings > Developer > Edit Config
2. Add the following configuration:

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

3. Save and restart Claude Desktop

The hammer icon in the chat window indicates available tools. Kite tools should be listed there.

A [video walkthrough](https://www.youtube.com/watch?v=tD1z8lR0CDE) of the setup process is available.

## Linux

There is no official Linux build. Community options include [claude-desktop-debian](https://github.com/aaddrick/claude-desktop-debian) for Debian/Ubuntu and [claude-desktop-linux-flake](https://github.com/k3d3/claude-desktop-linux-flake) for Nix.

The configuration file is located at `~/.config/Claude/claude_desktop_config.json`.
