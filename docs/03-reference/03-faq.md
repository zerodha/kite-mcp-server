---
title: FAQ
description: Frequently asked questions about Kite MCP
---

# FAQ

## General

**Is Kite MCP official?**
Yes. Kite MCP is developed and maintained by Zerodha. The source code is available on [GitHub](https://github.com/zerodha/kite-mcp-server).

**Which AI clients are supported?**
Any MCP-compatible client. Setup guides are available for [[Claude Desktop]], [[Claude Code]], [[Claude Web]], [[ChatGPT Web]], [[Cursor]], [[VS Code]], [[Windsurf]], [[Cline]], and [[JetBrains]]. See [[Other Clients]] for generic instructions.

**Is it free?**
Yes. A Zerodha account with Kite is all that is required.

**How long do sessions last?**
Up to 24 hours, matching the configured Kite session lifetime. Re-authenticate when the session expires.

## Security

**Can the AI access my Zerodha password?**
No. Authentication happens directly on Kite's login page. The AI client only receives a temporary session token after authorization.

**Can the AI place orders without confirmation?**
The AI will describe the intended action and request confirmation before placing, modifying, or cancelling orders.

**Can I self-host?**
Yes. Clone the [repository](https://github.com/zerodha/kite-mcp-server) and provide your own Kite Connect API credentials. See the README for setup instructions.

## Troubleshooting

**"Server disconnected"**
The connection timed out. Restart the AI client and try again.

**"Invalid session"**
The session has expired. Authenticate again.

**Tools not appearing**
Verify that Node.js is installed (`node --version`), the configuration JSON is valid, and the AI client has been restarted after adding the server.

**Login link not working**
Ensure pop-ups are not blocked. Copy the full URL and open it manually if needed.

**"Rate limit exceeded"**
Wait briefly and retry. Rate limits are in place to prevent abuse.

## Support

- [GitHub Issues](https://github.com/zerodha/kite-mcp-server/issues) for bugs and feature requests
- [Zerodha Support](https://support.zerodha.com/) for account-related issues
- [Kite Connect API documentation](https://kite.trade/docs/connect/v3/) for the underlying API
