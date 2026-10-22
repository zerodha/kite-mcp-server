---
description: Diagnose expired sessions, missing approval support and disabled tools.
---

# Authentication and access

- Authentication: use `session({"mode":"status"})`. Reauthenticate through the MCP client's OAuth flow when expired. `session({"mode":"logout"})` invalidates the current session; do not log out merely to diagnose an error.
- Unsupported approval client: reconnect with a client supporting form elicitation. A skill or chat message cannot substitute for transaction approval.
- Excluded tools: rely on the actual `tools/list`; a recipe documents capabilities but cannot enable a tool disabled by deployment configuration.
