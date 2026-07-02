---
title: Claude Code
description: Set up Kite MCP with Claude Code CLI
---

# Claude Code

Claude Code supports remote MCP servers natively over HTTP.

## Setup

```bash
claude mcp add kite --transport http https://mcp.kite.trade/mcp
```

Verify the server was added:

```bash
claude mcp list
```

## Scope

By default, the server is added to the current project. To make it available globally:

```bash
claude mcp add kite --transport http --scope user https://mcp.kite.trade/mcp
```

## Removing

```bash
claude mcp remove kite
```
