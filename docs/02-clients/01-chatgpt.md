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

## Order approvals

On ChatGPT web, review the approval form shown for order placement, modification,
or cancellation. Native forms do not currently work in this flow on the mobile
app; ask the assistant for a browser approval link instead.

Open the link in the browser used to connect Kite MCP and review the order there.
See [[Order Approvals]] for browser review and optional future order permission.

## Notes

- Custom connections must be enabled for your account or workspace
- ChatGPT handles OAuth and client registration automatically
- You normally do not need to create a client ID manually

## Troubleshooting

If sign-in fails, reconnect from the connection settings. If you cannot add a
custom connection, check your account access and workspace settings.

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that the answer comes from Kite. If no tool
is called, check that the connection is enabled for this conversation.
