package mcp

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
)

type MFHoldingsTool struct{}

func (*MFHoldingsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_mf_holdings",
		mcp.WithDescription("Get all mutual fund holdings. Supports pagination for large datasets."),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of MF holdings to return. If not specified, returns all holdings. When specified, response includes pagination metadata."),
		),
	)
}

func (*MFHoldingsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return PaginatedToolHandler(manager, "get_mf_holdings", func(session *kc.KiteSessionData) ([]interface{}, error) {
		holdings, err := session.Kite.Client.GetMFHoldings()
		if err != nil {
			return nil, err
		}

		// Convert to []interface{} for generic pagination
		result := make([]interface{}, len(holdings))
		for i, holding := range holdings {
			result[i] = holding
		}
		return result, nil
	})
}
