---
title: MCP Tool Eval
---

# MCP Tool Evaluation

Evaluates how well LLMs select the right tools and parameters when interacting
with Kite MCP via natural language prompts.

## Architecture

```
eval/run.sh
├── go run -tags=testing ./cmd/mockserver  (mock Kite API + full MCP server)
│   └── mock_responses/      (kiteconnect-mocks data)
├── pi -ne -e npm:pi-mcp-adapter  (LLM agent with MCP tools)
│   └── temporary MCP config (points to mock server)
└── eval/prompts.jsonl       (test prompts + expected tool calls)
```

The mock server runs the **real MCP tool code** - same handlers, same schemas,
same validation. Only the Kite Connect API responses are mocked using the
official kiteconnect-mocks fixtures.

## Running

```bash
cd /path/to/kite-mcp-server

# Terminal 1: start mock server
go run -tags=testing ./cmd/mockserver

# Terminal 2: run eval
./eval/run.sh
```

Or all-in-one:

```bash
./eval/run.sh --start-server
```

## Prompt format

`eval/prompts.jsonl` - one JSON object per line:

```json
{"prompt": "What's the current price of Infosys?", "expected_tool": "market", "expected_mode": "ltp"}
{"prompt": "Buy 10 shares of INFY", "expected_tool": "orders", "expected_mode": "place"}
```

## How it works

1. Mock server starts with pre-seeded session and test JWT
2. pi connects via pi-mcp-adapter with directTools enabled (eight ordinary tool groups exposed; discovery may vary by client capabilities)
3. Each prompt is sent to pi with a system instruction to only make tool calls
4. The eval script captures which tool and mode pi selected
5. Results are compared against expected_tool/expected_mode
6. Summary printed with accuracy per tool and overall score

The runner creates a private temporary MCP config and removes it on exit. It does
not overwrite an existing `eval/mcp.json`. Results remain in the ignored
`eval/results.jsonl` file for inspection.
