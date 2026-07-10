# Agentic Account MVP (Kite)

Robinhood-style “agentic account” flow on top of this MCP server: connect an AI agent, cap how much it can trade, and disconnect when done.

## What you already have

| Robinhood step | This repo |
|----------------|-----------|
| Connect via MCP | `http://localhost:8080/mcp` (or hosted `https://mcp.kite.trade/mcp`) |
| Login / authorize | `login` tool → Kite OAuth in browser |
| Agent places trades | `place_order`, `get_quotes`, `search_instruments`, etc. |
| Disconnect | `disconnect_agent` (MVP) |

## What the MVP adds

With `AGENTIC_ENABLED=true`:

- **`setup_agent_account`** — set a dedicated INR budget for this agent session
- **`get_agent_account`** — budget, spent, remaining
- **`disconnect_agent`** — clear budget + log out Kite for this session
- **`place_order`** — blocked if estimated order value exceeds remaining budget

## Quick start

### 1. Configure

```env
KITE_API_KEY=your_key
KITE_API_SECRET=your_secret
APP_MODE=http
APP_PORT=8080
AGENTIC_ENABLED=true
# Do NOT exclude place_order — agent needs it
EXCLUDED_TOOLS=
```

### 2. Run

```bash
go run main.go
```

### 3. Connect Cursor (or Claude)

Add to Cursor MCP settings (`~/.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "kite-agent": {
      "command": "npx",
      "args": ["mcp-remote", "http://localhost:8080/mcp", "--allow-http"]
    }
  }
}
```

### 4. Agent flow (example prompt)

Ask your agent:

1. **Login** — call `login`, open the link, complete Kite auth  
2. **Fund the agentic account** — `setup_agent_account` with `budget_inr: 50000` (or whatever cap you want)  
3. **Research** — `search_instruments`, `get_quotes`  
4. **Trade** — `place_order` (MARKET/LIMIT as needed); over-budget orders are rejected  
5. **Monitor** — `get_agent_account`, `get_orders`, `get_holdings`  
6. **Stop** — `disconnect_agent` when finished  

Example trade (agent must fill real symbol/params):

```text
Place a market BUY for 1 share of RELIANCE on NSE, product CNC, after confirming quote and budget.
```

### 5. Safety notes

- Budget is **per MCP session** (in-memory; resets on server restart).
- Estimates use LTP for market orders; actual fill may differ slightly.
- This is **real money** on your Kite account — start with a small `budget_inr`.
- Hosted `mcp.kite.trade` excludes `place_order`; run **self-hosted** for agentic trading.

## Tool reference

| Tool | Purpose |
|------|---------|
| `login` | Kite OAuth |
| `setup_agent_account` | Set `budget_inr` cap |
| `get_agent_account` | Budget status |
| `place_order` | Execute trade (budget-guarded when agentic enabled) |
| `disconnect_agent` | Revoke agent access for this session |

## Tests

Unit tests cover budget math, order blocking, MCP tool handlers, and session cleanup:

```bash
go test ./agentic/... ./mcp/... ./kc/... -run 'Agentic|Estimate|CheckAgentic|SetupAgent|GetAgent|Disconnect|Store|Cleanup'
```

What tests **do not** cover (requires your Kite credentials + manual run):

- Full OAuth `login` flow in a browser
- Live `place_order` execution on the exchange
- Hosted `mcp.kite.trade` (trading tools excluded there)

## Out of scope (full product)

- Separate brokerage sub-account
- Push notifications per trade
- Mobile dashboard UI
- Paper trading sandbox

These can be layered later; the MVP proves **MCP + budget + trade + disconnect** end-to-end.
