# Kite MCP Server

## Claude config:

The path to the config file can be found in the `claude_desktop_config.json` file.

Linux: `~/.config/Claude/claude_desktop_config.json`

### stdio mode:

```json
{
  "mcpServers": {
    "kite": {
      "command": "<ABSOLUTE_PATH>/run-kite-mcp.sh",
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
