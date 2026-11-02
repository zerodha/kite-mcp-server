---
title: JetBrains
description: Connect JetBrains AI Assistant to Kite MCP with an OAuth bridge
---

# JetBrains IDEs

Connect JetBrains AI Assistant through `mcp-remote`, which handles Kite's browser
sign-in and forwards MCP calls over Streamable HTTP. This setup requires
[Node.js](https://nodejs.org/).

## Setup

1. Open **Settings > Tools > AI Assistant > Model Context Protocol (MCP)**.
2. Add a server, choose **STDIO**, and enter:

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

3. Choose project or global scope, save, and start the connection.
4. Complete the Kite login in your browser when the bridge prompts, then return
   to AI Assistant and check the connection status.

AI Assistant also offers a native HTTP transport. The bridge above provides
Kite's OAuth sign-in without requiring a manually supplied bearer token.
See the [official JetBrains MCP guide](https://www.jetbrains.com/help/ai-assistant/mcp.html)
for IDE configuration controls.

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that your connection returned Kite data.
For order placement, modification, and cancellation, see [[Order Approvals]].
