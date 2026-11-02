---
description: Diagnose expired sessions, missing approval support and disabled tools.
---

# Authentication and access

- Authentication: use `session({"mode":"status"})`. Reauthenticate through the MCP client's OAuth flow when expired. `session({"mode":"logout"})` invalidates the current session; do not log out merely to diagnose an error.
- Trade approvals: order placement, modification, and cancellation require form elicitation. Clients without form support cannot execute these mutations. A declined or failed elicitation blocks execution and never falls back to direct execution.
- Excluded tools: rely on the actual `tools/list`; a recipe documents capabilities but cannot enable a tool disabled by deployment configuration.
