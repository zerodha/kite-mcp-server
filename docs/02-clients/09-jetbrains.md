---
title: JetBrains
description: Set up Kite MCP with IntelliJ, PyCharm, WebStorm, and other JetBrains IDEs
---

# JetBrains IDEs

JetBrains AI Assistant supports MCP servers across IntelliJ IDEA, PyCharm, WebStorm, and other JetBrains IDEs (2026+).

## Configuration

1. Open Settings > AI > MCP Servers (or Tools > AI Assistant > Model Context Protocol)
2. Click "Add"
3. Select SSE transport and enter the URL: `https://mcp.kite.trade/mcp`
4. Verify the Status column shows "Connected"

If native remote transport is not available, use stdio with `mcp-remote` (requires [Node.js](https://nodejs.org/)):

1. Click "Add" and select STDIO transport
2. Set command to `npx` and arguments to `mcp-remote https://mcp.kite.trade/mcp`
