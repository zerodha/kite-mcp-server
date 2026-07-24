package mcp

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
)

type ProfileTool struct{}

func (*ProfileTool) Tool() mcp.Tool {
	return mcp.NewTool("get_profile",
		mcp.WithDescription("Retrieve the user's profile information, including user ID, name, email, and account details like products orders, and exchanges available to the user. Use this to get basic user details."),
	)
}

func (*ProfileTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return SimpleToolHandler(manager, "get_profile", func(session *kc.KiteSessionData) (interface{}, error) {
		return session.Kite.Client.GetUserProfile()
	})
}

type MarginsTool struct{}

func (*MarginsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_margins",
		mcp.WithDescription("Get margins"),
	)
}

func (*MarginsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return SimpleToolHandler(manager, "get_margins", func(session *kc.KiteSessionData) (interface{}, error) {
		return session.Kite.Client.GetUserMargins()
	})
}

type HoldingsTool struct{}

func (*HoldingsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_holdings",
		mcp.WithDescription("Get holdings for the current user. Supports pagination for large datasets."),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of holdings to return. If not specified, returns all holdings. When specified, response includes pagination metadata."),
		),
	)
}

func (*HoldingsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return PaginatedToolHandler(manager, "get_holdings", func(session *kc.KiteSessionData) ([]interface{}, error) {
		holdings, err := session.Kite.Client.GetHoldings()
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

type PositionsTool struct{}

func (*PositionsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_positions",
		mcp.WithDescription("Get current positions. Supports pagination for large datasets."),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of positions to return. If not specified, returns all positions. When specified, response includes pagination metadata."),
		),
	)
}

func (*PositionsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return PaginatedToolHandler(manager, "get_positions", func(session *kc.KiteSessionData) ([]interface{}, error) {
		positions, err := session.Kite.Client.GetPositions()
		if err != nil {
			return nil, err
		}

		// Convert to []interface{} for generic pagination
		result := make([]interface{}, len(positions.Day)+len(positions.Net))
		idx := 0
		for _, pos := range positions.Day {
			result[idx] = pos
			idx++
		}
		for _, pos := range positions.Net {
			result[idx] = pos
			idx++
		}
		return result, nil
	})
}

type TradesTool struct{}

func (*TradesTool) Tool() mcp.Tool {
	return mcp.NewTool("get_trades",
		mcp.WithDescription("Get executed trades for the current trading day only. Kite Connect's trades endpoint is a transient intraday tradebook and does not return historical trades or all account transactions. Supports pagination for large datasets."),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of current-day trades to return. If not specified, returns all current-day trades. When specified, response includes pagination metadata."),
		),
	)
}

func (*TradesTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return PaginatedToolHandler(manager, "get_trades", func(session *kc.KiteSessionData) ([]interface{}, error) {
		trades, err := session.Kite.Client.GetTrades()
		if err != nil {
			return nil, err
		}

		// Convert to []interface{} for generic pagination
		result := make([]interface{}, len(trades))
		for i, trade := range trades {
			result[i] = trade
		}
		return result, nil
	})
}

type OrdersTool struct{}

func (*OrdersTool) Tool() mcp.Tool {
	return mcp.NewTool("get_orders",
		mcp.WithDescription("Get orders for the current trading day only, including open, pending, executed, cancelled, and rejected orders. Kite Connect's order book is transient and does not return historical orders across days. Supports pagination for large datasets."),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of current-day orders to return. If not specified, returns all current-day orders. When specified, response includes pagination metadata."),
		),
	)
}

func (*OrdersTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return PaginatedToolHandler(manager, "get_orders", func(session *kc.KiteSessionData) ([]interface{}, error) {
		orders, err := session.Kite.Client.GetOrders()
		if err != nil {
			return nil, err
		}

		// Convert to []interface{} for generic pagination
		result := make([]interface{}, len(orders))
		for i, order := range orders {
			result[i] = order
		}
		return result, nil
	})
}

type GTTOrdersTool struct{}

func (*GTTOrdersTool) Tool() mcp.Tool {
	return mcp.NewTool("get_gtts",
		mcp.WithDescription("Get all active GTT orders. Supports pagination for large datasets."),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of GTT orders to return. If not specified, returns all GTT orders. When specified, response includes pagination metadata."),
		),
	)
}

func (*GTTOrdersTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return PaginatedToolHandler(manager, "get_gtts", func(session *kc.KiteSessionData) ([]interface{}, error) {
		gttBook, err := session.Kite.Client.GetGTTs()
		if err != nil {
			return nil, err
		}

		// Convert to []interface{} for generic pagination
		result := make([]interface{}, len(gttBook))
		for i, gtt := range gttBook {
			result[i] = gtt
		}
		return result, nil
	})
}

type OrderTradesTool struct{}

func (*OrderTradesTool) Tool() mcp.Tool {
	return mcp.NewTool("get_order_trades",
		mcp.WithDescription("Get trades for a specific order"),
		mcp.WithString("order_id",
			mcp.Description("ID of the order to fetch trades for"),
			mcp.Required(),
		),
	)
}

func (*OrderTradesTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "get_order_trades")
		args := request.GetArguments()

		// Validate required parameters
		if err := ValidateRequired(args, "order_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		orderID := SafeAssertString(args["order_id"], "")

		return handler.WithSession(ctx, "get_order_trades", func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
			orderTrades, err := session.Kite.Client.GetOrderTrades(orderID)
			if err != nil {
				return mcp.NewToolResultError("Failed to get order trades"), nil
			}

			return handler.MarshalResponse(orderTrades, "get_order_trades")
		})
	}
}

type OrderHistoryTool struct{}

func (*OrderHistoryTool) Tool() mcp.Tool {
	return mcp.NewTool("get_order_history",
		mcp.WithDescription("Get status history for a specific current-day order. Requires an order_id and does not list historical orders across days."),
		mcp.WithString("order_id",
			mcp.Description("ID of the order to fetch history for"),
			mcp.Required(),
		),
	)
}

func (*OrderHistoryTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "get_order_history")
		args := request.GetArguments()

		// Validate required parameters
		if err := ValidateRequired(args, "order_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		orderID := SafeAssertString(args["order_id"], "")

		return handler.WithSession(ctx, "get_order_history", func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
			orderHistory, err := session.Kite.Client.GetOrderHistory(orderID)
			if err != nil {
				return mcp.NewToolResultError("Failed to get order history"), nil
			}

			return handler.MarshalResponse(orderHistory, "get_order_history")
		})
	}
}

type TradebookAvailabilityTool struct{}

type tradebookAvailabilityResponse struct {
	Available           bool     `json:"available"`
	Scope               string   `json:"scope"`
	CurrentDayTools     []string `json:"current_day_tools"`
	HistoricalDataTools  []string `json:"historical_data_tools"`
	Message             string   `json:"message"`
	RecommendedResponse string   `json:"recommended_response"`
}

func (*TradebookAvailabilityTool) Tool() mcp.Tool {
	return mcp.NewTool("get_tradebook",
		mcp.WithDescription("Explain whether Kite MCP can fetch a complete historical stock transaction tradebook. This tool does not call a Kite API endpoint; it reports that Kite Connect exposes only current-day orders/trades through this server and points to the available alternatives."),
	)
}

func (*TradebookAvailabilityTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if manager != nil {
			handler.trackToolCall(ctx, "get_tradebook")
		}

		response := tradebookAvailabilityResponse{
			Available: false,
			Scope:     "Kite Connect does not expose an API endpoint through this server for fetching all historical stock transactions or the full account tradebook across days.",
			CurrentDayTools: []string{
				"get_orders: current trading day's orders only",
				"get_trades: current trading day's executed trades only",
				"get_order_history: status history for one current-day order_id",
				"get_order_trades: trades for one current-day order_id",
			},
			HistoricalDataTools: []string{
				"get_historical_data: historical OHLC candle data for an instrument, not account transactions",
			},
			Message:             "There is no Kite MCP tool that fetches all historical stock transactions or the complete tradebook. Use the current-day order/trade tools for intraday activity; use Kite/Console reports outside Kite Connect for historical contract notes, ledger, tax P&L, or full transaction history.",
			RecommendedResponse: "No. Kite MCP cannot fetch your complete historical stock transaction tradebook. get_orders and get_trades cover only the current trading day, while get_order_history and get_order_trades require a specific current-day order_id.",
		}

		v, err := json.Marshal(response)
		if err != nil {
			return mcp.NewToolResultError("Failed to process tradebook availability response"), nil
		}

		return mcp.NewToolResultText(string(v)), nil
	}
}
