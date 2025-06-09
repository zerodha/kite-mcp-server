package mcp

import (
	"context"

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
		mcp.WithDescription("Get trading history. Supports pagination for large datasets."),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of trades to return. If not specified, returns all trades. When specified, response includes pagination metadata."),
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
		mcp.WithDescription("Get all orders. Supports pagination for large datasets."),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of orders to return. If not specified, returns all orders. When specified, response includes pagination metadata."),
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
		mcp.WithDescription("Get order history for a specific order"),
		mcp.WithString("order_id",
			mcp.Description("ID of the order to fetch history for"),
			mcp.Required(),
		),
	)
}

func (*OrderHistoryTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
