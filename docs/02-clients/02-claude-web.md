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

If you cannot add a connector, check whether your organisation allows custom connectors. If sign-in fails, reconnect through the client settings.

See [Claude’s official connector guide](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp) for the current settings flow.

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that the answer comes from Kite. If no tool
is called, check that the connection is enabled for this conversation.
