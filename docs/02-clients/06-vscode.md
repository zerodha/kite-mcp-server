---
title: VS Code
description: Set up Kite MCP with VS Code and GitHub Copilot
---

# VS Code

VS Code supports remote MCP servers natively over HTTP through GitHub Copilot. No additional bridge or Node.js installation is required.

## Prerequisites

- A current version of [VS Code](https://code.visualstudio.com/) with MCP support
- GitHub Copilot extension

## Configuration

1. Open Command Palette (`Ctrl+Shift+P`)
2. Run `MCP: Add Server`, choose HTTP, and enter `https://mcp.kite.trade/mcp`.
3. Name the server `kite` and choose its configuration scope. Review the trust and OAuth sign-in prompts.

For the VS Code-format `.vscode/mcp.json` or user-profile `mcp.json`, the entry is:

```json
{
    "servers": {
        "kite": {
            "url": "https://mcp.kite.trade/mcp",
            "type": "http"
        }
    },
    "inputs": []
}
```

Configuration formats vary by agent harness. The `servers` format above is for VS Code configuration; portable Copilot configurations use `mcpServers`. Use the guided setup to select the right destination. Save the file and start or restart the server through its MCP controls.

Open Copilot Chat in Agent mode to begin. Authorize the Zerodha account when prompted.

Refer to the [VS Code MCP documentation](https://code.visualstudio.com/docs/agent-customization/mcp-servers) for additional configuration options.

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that the answer comes from Kite. If no tool
is called, check that the connection is enabled for this conversation.
