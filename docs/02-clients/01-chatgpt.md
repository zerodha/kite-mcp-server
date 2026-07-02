---
title: ChatGPT Web
description: Set up Kite MCP with ChatGPT remote MCP connectors
---

# ChatGPT Web

ChatGPT can connect to Kite MCP as a remote MCP server when the feature is enabled for your account.

## Setup

1. Open ChatGPT settings
2. Go to connectors, integrations, or developer tools depending on the current UI
3. Create a new custom connector or MCP server
4. Enter the server URL:

```text
https://mcp.kite.trade/mcp
```

5. Choose OAuth authentication
6. Save and start the connection flow
7. Complete the Zerodha login flow in the browser when prompted

## Notes

- Remote MCP support in ChatGPT is still rolling out and UI labels may change
- ChatGPT handles OAuth and client registration automatically
- You normally do not need to create a client ID manually

## Troubleshooting

If the connector is rejected during login, try removing and recreating it. Self-hosted deployments must allow ChatGPT callback URIs during dynamic client registration.
