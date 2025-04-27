# Kite MCP Server

## Claude config:

The path to the config file can be found in the `claude_desktop_config.json` file.

Linux: `~/.config/Claude/claude_desktop_config.json`

### stdio mode:

```json
{
  "mcpServers": {
    "kite": {
      "command": "go",
      "args": [
        "run",
        "<ABSOLUTE_PATH>/main.go",
      ],
      "env": {
        "APP_MODE": "stdio",
        "KITE_API_KEY": "<your_api_key>",
        "KITE_API_SECRET": "<your_api_secret>"
      }
    }
  }
}
```

### SSE mode

For the SSE mode, you can run the following command to start the server:

```
go run main.go
```

```json
{
  "mcpServers": {
    "remote-example": {
      "command": "npx",
      "args": ["mcp-remote", "http://localhost:8081/sse"]
    }
  }
}
```

If you want to use the hosted version, you can use the following config:

```json
{
  "mcpServers": {
    "remote-example": {
      "command": "npx",
      "args": ["mcp-remote", "http://api.kite.trade/sse"]
    }
  }
}
```
