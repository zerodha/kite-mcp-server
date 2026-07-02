---
title: Claude Web
description: Set up Kite MCP with Claude hosted connectors
---

# Claude Web

Claude hosted surfaces can connect to Kite MCP as a remote MCP server when connector support is available for your account.

## Setup

1. Open Claude settings or connector management
2. Add a new remote MCP server or hosted connector
3. Enter the server URL:

```text
https://mcp.kite.trade/mcp
```

4. Continue with OAuth authentication
5. Complete the Zerodha login flow in the browser when prompted

## Notes

- Claude handles OAuth and client registration automatically
- You normally do not need to create a client ID manually
- Product labels may vary between Claude surfaces and rollout stages

## Troubleshooting

If the OAuth flow fails on a self-hosted deployment, confirm that the deployment allows Claude callback URIs during dynamic client registration.
