package mcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

type PortfolioTool struct{}

// Keep the upstream views distinguishable when paginating their combined rows.
// Day reports today's activity; Net is the authoritative current exposure.
type positionWithView struct {
	kiteconnect.Position
	View string `json:"view"`
}

var portfolioSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"mode": {
			"type": "string",
			"description": "Operation mode: profile=user profile and account details, margins=available funds and margins across segments, holdings=demat holdings with P&L, positions=open intraday and overnight positions",
			"enum": ["profile", "margins", "holdings", "positions"]
		},
		"type": {
			"type": "string",
			"description": "Holdings data type (only for mode=holdings). 'full' returns detailed holdings, 'summary' returns aggregated data, 'compact' returns minimal fields",
			"default": "full",
			"enum": ["full", "summary", "compact"]
		},
		"from": {
			"type": "integer",
			"minimum": 0,
			"maximum": 2147483647,
			"default": 0,
			"description": "Starting index for pagination (0-based). Applies to holdings (full/compact) and positions modes. Default: 0"
		},
		"limit": {
			"type": "integer",
			"minimum": 1,
			"maximum": 500,
			"default": 100,
			"description": "Page size (integer 1-500, default 100). Responses always include pagination metadata"
		}
	},
	"required": ["mode"]
}`)

func (*PortfolioTool) Definition() *mcp.Tool {
	return NewTool("portfolio",
		"Retrieve portfolio and account information. Use mode=profile for user details (name, email, account type), mode=margins for available funds and margins, mode=holdings for demat holdings with P&L, mode=positions for open intraday and overnight positions. Position rows include view=day for today's activity or view=net for current exposure; do not sum the two views. Full/compact holdings and positions return data/pagination with default limit 100 and maximum 500. Summary holdings are unpaginated.",
		portfolioSchema,
	)
}

func (*PortfolioTool) Handler(manager *kc.Manager) ToolHandler {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := GetArguments(request)
		mode := SafeAssertString(args["mode"], "")

		switch mode {
		case "profile":
			return handler.WithKiteClient(request, "portfolio_profile", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				profile, err := client.GetUserProfile()
				if err != nil {
					return NewToolResultError("Failed to get profile"), nil
				}
				return handler.MarshalResponse(profile, "portfolio_profile")
			})

		case "margins":
			return handler.WithKiteClient(request, "portfolio_margins", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				margins, err := client.GetUserMargins()
				if err != nil {
					return NewToolResultError("Failed to get margins"), nil
				}
				return handler.MarshalResponse(margins, "portfolio_margins")
			})

		case "holdings":
			params, err := ParsePaginationParams(args)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return handler.WithKiteClient(request, "portfolio_holdings", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				holdingsType := SafeAssertString(args["type"], "full")

				switch holdingsType {
				case "summary":
					summary, err := client.GetHoldingsSummary()
					if err != nil {
						return NewToolResultError("Failed to get holdings summary"), nil
					}
					return handler.MarshalResponse(summary, "portfolio_holdings")

				case "compact":
					compactHoldings, err := client.GetHoldingsCompact()
					if err != nil {
						return NewToolResultError("Failed to get compact holdings"), nil
					}
					result := make([]interface{}, len(compactHoldings))
					for i, h := range compactHoldings {
						result[i] = h
					}
					page := ApplyPagination(result, params)
					return handler.MarshalResponse(CreatePaginatedResponse(page, params, len(result)), "portfolio_holdings")

				default: // "full"
					holdings, err := client.GetHoldings()
					if err != nil {
						return NewToolResultError("Failed to get holdings"), nil
					}
					result := make([]interface{}, len(holdings))
					for i, h := range holdings {
						result[i] = h
					}
					page := ApplyPagination(result, params)
					return handler.MarshalResponse(CreatePaginatedResponse(page, params, len(result)), "portfolio_holdings")
				}
			})

		case "positions":
			return PaginatedToolHandler(manager, "portfolio_positions", func(client *kiteconnect.Client) ([]interface{}, error) {
				positions, err := client.GetPositions()
				if err != nil {
					return nil, err
				}
				result := make([]interface{}, 0, len(positions.Day)+len(positions.Net))
				for _, pos := range positions.Day {
					result = append(result, positionWithView{Position: pos, View: "day"})
				}
				for _, pos := range positions.Net {
					result = append(result, positionWithView{Position: pos, View: "net"})
				}
				return result, nil
			})(ctx, request)

		default:
			return NewToolResultError("Invalid mode. Must be one of: profile, margins, holdings, positions"), nil
		}
	}
}
