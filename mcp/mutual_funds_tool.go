package mcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

type MutualFundsTool struct{}

var mutualFundsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"mode": {
			"type": "string",
			"description": "Operation mode: holdings=mutual fund holdings via Coin",
			"enum": ["holdings"]
		}
	},
	"required": ["mode"]
}`)

func (*MutualFundsTool) Definition() *mcp.Tool {
	return NewTool("mutual_funds",
		"Retrieve mutual fund data from Coin (Zerodha's mutual fund platform). Use mode=holdings to get all mutual fund holdings.",
		mutualFundsSchema,
	)
}

func (*MutualFundsTool) Handler(manager *kc.Manager) ToolHandler {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := GetArguments(request)
		mode := SafeAssertString(args["mode"], "")

		switch mode {
		case "holdings":
			return handler.WithKiteClient(request, "mutual_funds_holdings", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				holdings, err := client.GetMFHoldings()
				if err != nil {
					return NewToolResultError("Failed to get mutual fund holdings"), nil
				}
				return handler.MarshalResponse(holdings, "mutual_funds_holdings")
			})

		default:
			return NewToolResultError("Invalid mode. Must be one of: holdings"), nil
		}
	}
}
