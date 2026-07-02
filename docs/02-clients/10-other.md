---
title: Other Clients
description: Using Kite MCP with any MCP-compatible client
---

# Other Clients

Any MCP-compatible client can connect to Kite MCP. The server URL is:

```
https://mcp.kite.trade/mcp
```

## Clients with HTTP/streamable-http support

Configure the server URL directly. No bridge is required. This applies to VS Code (Copilot), Claude Code, and recent versions of most clients.

## Clients with stdio-only support

Use [mcp-remote](https://www.npmjs.com/package/mcp-remote) to bridge the connection. Requires [Node.js](https://nodejs.org/).

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

The JSON structure varies by client, but the `command` and `args` values remain the same.

## Compatible clients

- [[Claude Desktop]] (stdio + mcp-remote)
- [[Claude Code]] (native HTTP)
- [[Claude Web]] (hosted remote MCP)
- [[ChatGPT Web]] (hosted remote MCP)
- [[Cursor]] (stdio + mcp-remote)
- [[VS Code]] (native HTTP via Copilot)
- [[Windsurf]] (streamable-http or mcp-remote)
- [[Cline]] (remote URL or mcp-remote)
- [[JetBrains]] (SSE or mcp-remote)
- [Zed](https://zed.dev/) (extension-based MCP)
- [Continue.dev](https://continue.dev/) (VS Code/JetBrains extension)
- [Roo Code](https://roocode.com/) (VS Code extension, streamable-http)
