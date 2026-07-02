---
title: VS Code
description: Set up Kite MCP with VS Code and GitHub Copilot
---

# VS Code

VS Code supports remote MCP servers natively over HTTP through GitHub Copilot. No additional bridge or Node.js installation is required.

## Prerequisites

- [VS Code](https://code.visualstudio.com/) 1.102.0 or newer
- GitHub Copilot extension

## Configuration

1. Open Command Palette (`Ctrl+Shift+P`)
2. Run `MCP: Open User Configuration`
3. Add the following configuration:

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

4. Save and restart VS Code

Open Copilot Chat in Agent mode to begin. Authorize the Zerodha account when prompted.

Refer to the [VS Code MCP documentation](https://code.visualstudio.com/docs/copilot/customization/mcp-servers) for additional configuration options.
