---
title: Other Clients
description: Using Kite MCP with any MCP-compatible client
---

# Other Clients

Use a client with Streamable HTTP and OAuth support, or a stdio client with
the `mcp-remote` bridge. The server URL is:

```
https://mcp.kite.trade/mcp
```

## Direct HTTP connections

Configure the server URL directly. No bridge is required. This applies to VS Code (Copilot), Claude Code, Codex CLI, and recent versions of most clients.

Choose **Streamable HTTP** when offered a transport choice for this endpoint.

## Stdio bridge

Use [mcp-remote](https://www.npmjs.com/package/mcp-remote) to bridge the connection. Requires [Node.js](https://nodejs.org/).

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

The JSON structure varies by client, but the `command` and `args` values remain the same.

## Order approval support

Clients with **form elicitation** can display order approvals in the conversation.
Other clients receive a browser link when an order needs approval. Open it in
any browser, sign in to your Kite account, and review the action there.

The browser page also lets you optionally allow future orders from that client.
See [[Order Approvals]] for scope, expiry, and revocation.

## Compatible clients

- [[Claude Desktop]] (remote connector or stdio bridge)
- [[Claude Code]] (native HTTP)
- [[Codex CLI]] (native HTTP)
- [[Pi]] (native HTTP and code mode)
- [[Claude Web]] (hosted remote MCP)
- [[ChatGPT Web]] (hosted remote MCP)
- [[Cursor]] (native HTTP or stdio bridge)
- [[VS Code]] (native HTTP via Copilot)
- [[Windsurf]] (streamable-http or mcp-remote)
- [[Cline]] (remote URL or mcp-remote)
- [[JetBrains]] (stdio bridge for Kite OAuth)
- [Zed](https://zed.dev/) (extension-based MCP)
- [Continue.dev](https://continue.dev/) (VS Code/JetBrains extension)

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that the answer comes from Kite. If no tool
is called, check that the connection is enabled for this conversation.
