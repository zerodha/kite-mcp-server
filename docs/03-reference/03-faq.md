---
title: FAQ
description: Frequently asked questions about Kite MCP
---

# FAQ

## General

### Is Kite MCP official?
Yes. Kite MCP is developed and maintained by Zerodha. The source code is available on [GitHub](https://github.com/zerodha/kite-mcp-server).

### Which AI clients are supported?
Clients need remote MCP support, either directly or through a stdio bridge. Optional features such as approval forms and interactive views vary by client. Setup guides are available for [[Claude Desktop]], [[Claude Code]], [[Codex CLI]], [[Pi]], [[Claude Web]], [[ChatGPT Web]], [[Cursor]], [[VS Code]], [[Windsurf]], [[Cline]], and [[JetBrains]]. See [[Other Clients]] for generic instructions.

### Is it free?
Yes. The hosted Kite MCP connection is free and requires a Zerodha account with Kite. Your AI client may have its own subscription or access requirements. Self-hosted installations need their own Kite Connect API credentials.

### How long do sessions last?
Up to seven days from login. Kite credential renewal does not extend that deadline. Automatic renewal requires a Kite-issued refresh token; without one, you must log in again when Kite access expires. A server restart also requires a new login because sessions are kept in memory.

## Security

### Can the AI access my Zerodha password?
No. Authentication happens directly on Kite's login page. The AI client only receives a temporary session token after authorization.

### Can the AI place orders without confirmation?
Order placement, modification, and cancellation require a client form or browser
approval by default. You can explicitly allow future orders from this client on
the browser page. While enabled, those requests execute without asking again.
See [[Order Approvals]] for expiry and how to revoke this permission.

GTT and Alerts/ATO mutations are outside this guard and can create future trades.
Only request changes you intend, and check which tools your server exposes.

### Can I self-host?
Yes. Clone the [repository](https://github.com/zerodha/kite-mcp-server) and provide your own Kite Connect API credentials. See the README for setup instructions.

## Troubleshooting

### "Server disconnected"
Check the server connection in your client and reconnect if needed. An expired login, network interruption, or server restart can disconnect a session.

### "Invalid session"
The session has expired. Authenticate again.

### Tools not appearing
Enable Kite MCP for the current conversation and check the client-specific configuration and organisation settings. Native HTTP setups do not require Node.js. If you use `mcp-remote`, verify Node.js is installed and that the bridge starts successfully.

### Login link not working
Ensure pop-ups are not blocked. Copy the full URL and open it manually if needed.

### "Rate limit exceeded"
For data reads, wait before retrying and honour any `Retry-After` response. For
an order submission or other mutation, check its status before requesting
another attempt. Do not automatically resubmit a trade.

## Support

- [GitHub Discussions](https://github.com/zerodha/kite-mcp-server/discussions) for bugs and feature requests
- [Zerodha Support](https://support.zerodha.com/) for account-related issues
- [Kite Connect API documentation](https://kite.trade/docs/connect/v3/) for the underlying API
