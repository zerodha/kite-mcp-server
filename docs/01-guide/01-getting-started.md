---
title: Quick Start
description: Set up Kite MCP with your AI assistant
---

# Quick Start

## Prerequisites

- A Zerodha account with Kite
- An MCP-compatible AI client

Optional, depending on client:

- [Node.js](https://nodejs.org/) if your client needs `mcp-remote`
- A desktop MCP client such as Claude Desktop, Claude Code, VS Code, or Cursor if you are not using a hosted web client

## Supported clients

If you want the easiest setup, start with hosted remote clients:

- [[ChatGPT Web]]
- [[Claude Web]]

Detailed setup instructions are also available for:

- [[Claude Desktop]]
- [[Claude Code]]
- [[Cursor]]
- [[VS Code]]
- [[Windsurf]]
- [[Cline]]
- [[JetBrains]]
- [[Other Clients]]

## Server URL

All clients connect to the same endpoint:

```
https://mcp.kite.trade/mcp
```

If your client supports project-local MCP config, this is a standard setup:

```json
{
  "mcpServers": {
    "kite-mcp": {
      "type": "http",
      "url": "https://mcp.kite.trade/mcp"
    }
  }
}
```

### Fastest setup path

For most users, [[ChatGPT Web]] and [[Claude Web]] are the fastest setup path because they use a hosted remote MCP flow and handle OAuth in the UI.

Clients with native HTTP transport support (VS Code, Claude Code, Windsurf) can connect directly. Clients that use stdio transport (Claude Desktop, Cursor) require [mcp-remote](https://www.npmjs.com/package/mcp-remote) as a bridge.

## Authentication

Most MCP clients handle OAuth automatically. On first use, the AI client will either open a browser window or show a login link. You will first see a consent form naming the client, its exact registered return address, and the account access it requests. Approve only a client and return address you recognize, then continue to Kite's login page in the same browser. The callback is bound to that browser and approval is single-use. After login, the client receives a temporary access token. Application sessions last up to seven days, unless the server restarts or you log out. Kite access is renewed automatically only when Kite issues a refresh token; otherwise another Kite login is required when its access token expires.

Your Zerodha credentials are never sent to the AI client or the MCP server. Authentication happens directly with Kite.

### Hosted remote MCP clients

Hosted clients such as [[ChatGPT Web]] and [[Claude Web]] usually use OAuth dynamic client registration automatically. In practice, this means:

- you provide the MCP server URL
- the client registers its own redirect URI with the server
- the server redirects you to Kite for login
- the client completes the OAuth flow after the browser callback

No manual client ID or JSON configuration is normally required on the user side.
