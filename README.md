# Kite MCP Server

## Claude config:

Linux: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "kite": {
      "command": "uv",
      "args": [
        "--directory",
        "<ABSOLUTE_PATH>/kite-mcp-server",
        "run",
        "server.py"
      ],
      "env": {
        "KITE_API_KEY": "<your_api_key>",
        "KITE_API_SECRET": "<your_api_secret>"
      }
    }
  }
}
```
