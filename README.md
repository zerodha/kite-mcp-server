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
      "args": ["run", "<ABSOLUTE_PATH>/main.go"],
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
    "kite": {
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
    "kite": {
      "command": "npx",
      "args": ["mcp-remote", "https://mcp.kite.trade/sse"]
    }
  }
}
```

## VS Code Setup

### Prerequisites

- Visual Studio Code installed ([download](https://code.visualstudio.com/download)).
- Node.js installed (version 18 or higher is recommended). You can download it from https://nodejs.org. (Note: older versions might cause issues, as seen in user feedback on the blog).
- VS Code GitHub Copilot extension installed and authenticated with your GitHub account. ([link](https://marketplace.visualstudio.com/items?itemName=GitHub.copilot))

### Configuration Steps for macOS

1. Open VS Code.
2. Open the Command Palette using `Cmd+Shift+P`.
3. Type "Preferences: Open User Settings (JSON)" and select it to open your `settings.json` file.
4. Add the following JSON configuration to your `settings.json` file. If you already have existing settings, add this as a new top-level entry, ensuring you place a comma appropriately if needed to keep the JSON valid:
   ```json
   "mcp": {
       "inputs": [],
       "servers": {
           "kite": {
               "url": "https://mcp.kite.trade/sse"
           }
       }
   }
   ```
   (Note: Ensure that the quotes are standard double quotes (`"`) and not curly quotes (`“”`), as curly quotes will cause parsing errors. Also, ensure the URL is `https://` and not `http://`.)
5. Save the `settings.json` file.
6. Restart Visual Studio Code for the changes to take effect.
7. Open the GitHub Copilot Chat panel (you can usually find this in the sidebar or by searching for "Copilot Chat" in the Command Palette).
8. In the chat input, type `/mcp` and press Enter. You should see "kite" listed as an available MCP server. This command helps verify that the MCP server is recognized by the Copilot extension.
9. If prompted, follow the on-screen instructions to authorize your Zerodha account. This will typically involve logging in to your Kite account through a browser window opened by VS Code.

### Troubleshooting

*   **Error: `MCP kite: Server disconnected` or `Could not attach to MCP server kite` or `session not found`**
    *   **Node.js Version:** Ensure you have Node.js version 18 or higher installed. You can check your version by opening a terminal and typing `node --version`. If it's lower than 18, please upgrade from [https://nodejs.org](https://nodejs.org).
    *   **Check `settings.json`:**
        *   Verify that the JSON in your `settings.json` file is correct. Pay close attention to commas (especially if you added the MCP config next to existing settings) and ensure you've used standard double quotes (`"`) for all keys and string values, not curly quotes (`“”`).
        *   Ensure the URL is exactly `https://mcp.kite.trade/sse`. Using `http://` will not work.
    *   **Restart VS Code:** Make sure you've restarted VS Code after making changes to `settings.json`.
    *   **Re-authenticate:** Try typing `/mcp` in the Copilot chat. If Kite is listed, try selecting it. It might prompt for re-authentication. Sometimes, the session might expire, and you might need to log in to Kite again.
    *   **Check for Conflicting Extensions:** While rare, another extension could potentially interfere. Try temporarily disabling other extensions related to chat or AI to see if that resolves the issue.

*   **Error related to `npx` not found or `spawn npx ENOENT` (this might appear in VS Code's logs or if trying to run mcp-remote manually for debugging)**
    *   **Node.js Installation:** `npx` is part of Node.js (npm). Ensure Node.js is installed correctly and that its installation directory (usually something like `C:\Program Files\nodejs` on Windows or `/usr/local/bin` on macOS/Linux) is included in your system's PATH environment variable. You can verify by opening a new terminal/command prompt and typing `npx --version`. If it doesn't show a version, your PATH is likely not configured correctly for Node.js.
    *   **Restart VS Code/Terminal:** After installing Node.js or modifying PATH, restart VS Code and any terminal windows.

*   **`/mcp` command not working or not showing Kite in Copilot Chat**
    *   **Copilot Extension:** Ensure the GitHub Copilot extension (and specifically GitHub Copilot Chat) is enabled and properly authenticated. Check for any error notifications from the Copilot extension itself.
    *   **Correct `settings.json` Entry:** Double-check that the `"mcp"` configuration is correctly placed in your `settings.json` file and that the file was saved.
    *   **VS Code Version:** Ensure your VS Code is updated to a recent version, as MCP support in extensions might depend on newer VS Code APIs.

*   **JSON Parsing Errors (e.g., "Error reading or parsing settings.json")**
    *   This is almost always due to a syntax error in your `settings.json` file.
    *   Carefully check for missing or extra commas, incorrect quote types (use `"` not `“”`), or mismatched brackets (`{}`).
    *   You can use an online JSON validator to check the syntax of your `settings.json` content if you're unsure.

## Kite Connect API Integration Status

| API Method                   | Integration Status | Remarks                                         |
| ---------------------------- | ------------------ | ----------------------------------------------- |
| **User & Account Methods**   |                    |                                                 |
| `GetUserProfile()`           | [x]                | Implemented as `get_profile` tool               |
| `GetUserMargins()`           | [x]                | Implemented as `get_margins` tool               |
| `GetHoldings()`              | [x]                | Implemented as `get_holdings` tool              |
| `GetPositions()`             | [x]                | Implemented as `get_positions` tool             |
| `GetFullUserProfile()`       | [ ]                | Not yet implemented                             |
| `InvalidateAccessToken()`    | [ ]                | Not yet implemented                             |
| `InvalidateRefreshToken()`   | [ ]                | Not yet implemented                             |
| `RenewAccessToken()`         | [ ]                | Not yet implemented                             |
| **Orders & Trades Methods**  |                    |                                                 |
| `GetOrders()`                | [x]                | Implemented as `get_orders` tool                |
| `GetTrades()`                | [x]                | Implemented as `get_trades` tool                |
| `PlaceOrder()`               | [x]                | Implemented as `place_order` tool               |
| `ModifyOrder()`              | [x]                | Implemented as `modify_order` tool              |
| `CancelOrder()`              | [x]                | Implemented as `cancel_order` tool              |
| `ExitOrder()`                | [ ]                | Not yet implemented                             |
| `ConvertPosition()`          | [ ]                | Not yet implemented                             |
| `GetOrderHistory()`          | [ ]                | Not yet implemented                             |
| `GetOrderTrades()`           | [ ]                | Not yet implemented                             |
| `GetOrderMargins()`          | [ ]                | Not yet implemented                             |
| `GetBasketMargins()`         | [ ]                | Not yet implemented                             |
| `GetOrderCharges()`          | [ ]                | Not yet implemented                             |
| **GTT Orders**               |                    |                                                 |
| `GetGTTs()`                  | [x]                | Implemented as `get_gtts` tool                  |
| `GetGTT()`                   | [ ]                | Not yet implemented                             |
| `PlaceGTT()`                 | [x]                | Implemented as `place_gtt_order` tool           |
| `ModifyGTT()`                | [x]                | Implemented as `modify_gtt_order` tool          |
| `DeleteGTT()`                | [x]                | Implemented as `delete_gtt_order` tool          |
| **Market Data Methods**      |                    |                                                 |
| `GetQuote()`                 | [x]                | Implemented as `get_quotes` tool                |
| `GetHistoricalData()`        | [x]                | Implemented as `get_historical_data` tool       |
| `GetLTP()`                   | [ ]                | Not yet implemented                             |
| `GetOHLC()`                  | [ ]                | Not yet implemented                             |
| `GetInstruments()`           | [-]                | Won't implement. Use `instruments_search` tool. |
| `GetInstrumentsByExchange()` | [-]                | Won't implement                                 |
| `GetAuctionInstruments()`    | [ ]                | Not yet implemented                             |
| **Mutual Funds Methods**     |                    |                                                 |
| `GetMFOrders()`              | [ ]                | Not yet implemented                             |
| `GetMFOrderInfo()`           | [ ]                | Not yet implemented                             |
| `PlaceMFOrder()`             | [ ]                | Not yet implemented                             |
| `CancelMFOrder()`            | [ ]                | Not yet implemented                             |
| `GetMFSIPs()`                | [ ]                | Not yet implemented                             |
| `GetMFSIPInfo()`             | [ ]                | Not yet implemented                             |
| `PlaceMFSIP()`               | [ ]                | Not yet implemented                             |
| `ModifyMFSIP()`              | [ ]                | Not yet implemented                             |
| `CancelMFSIP()`              | [ ]                | Not yet implemented                             |
| `GetMFHoldings()`            | [x]                | Implemented as `get_mf_holdings` tool           |
| `GetMFHoldingInfo()`         | [ ]                | Not yet implemented                             |
| `GetMFInstruments()`         | [ ]                | Not yet implemented                             |
| `GetMFOrdersByDate()`        | [ ]                | Not yet implemented                             |
| `GetMFAllottedISINs()`       | [ ]                | Not yet implemented                             |
| **Other Methods**            |                    |                                                 |
| `InitiateHoldingsAuth()`     | [ ]                | Not yet implemented                             |
| `GetUserSegmentMargins()`    | [ ]                | Not yet implemented                             |
