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
or cancellation. On the mobile app, the assistant should select browser review
automatically and share the approval link. You do not need to ask for a link
separately.

Open the link in any browser, sign in to your Kite account, and review the order.
See [[Order Approvals]] for browser review and optional future order permission.

## Notes

- Custom connections must be enabled for your account or workspace
- ChatGPT handles OAuth and client registration automatically
- You normally do not need to create a client ID manually

## Troubleshooting

If sign-in fails, reconnect from the connection settings. If you cannot add a
custom connection, check your account access and workspace settings.

If the assistant says `approval_method` or `approval_status` is missing while
Help describes browser approvals, refresh the Kite connection's tool metadata
in ChatGPT web and start a new conversation. Reconnecting alone may keep an old
tool schema. See the [official metadata refresh guidance](https://developers.openai.com/apps-sdk/deploy/connect-chatgpt#refresh-metadata).

## Verify the connection

> Use Kite MCP to show my account profile. Do not place any orders.

Inspect the tool result to confirm that the answer comes from Kite. If no tool
is called, check that the connection is enabled for this conversation.
