package mcp

import (
	"context"
	"encoding/json"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
)

type ProfileTool struct{}

func (*ProfileTool) Tool() mcp.Tool {
	return mcp.NewTool("get_profile",
		mcp.WithDescription("Get profile"),
	)
}

func (*ProfileTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting profile", err)
			return nil, err
		}

		profile, err := kc.Kite.Client.GetUserProfile()
		if err != nil {
			log.Println("error getting profile", err)
			return nil, err
		}

		v, err := json.Marshal(profile)
		if err != nil {
			log.Println("error marshalling profile", err)
			return nil, err
		}

		profileJSON := string(v)
		return mcp.NewToolResultText(profileJSON), nil
	}
}

type MarginsTool struct{}

func (*MarginsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_margins",
		mcp.WithDescription("Get margins"),
	)
}

func (*MarginsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting margins", err)
			return nil, err
		}

		margins, err := kc.Kite.Client.GetUserMargins()
		if err != nil {
			log.Println("error getting margins", err)
			return nil, err
		}

		v, err := json.Marshal(margins)
		if err != nil {
			log.Println("error marshalling margins", err)
			return nil, err
		}

		marginsJSON := string(v)
		return mcp.NewToolResultText(marginsJSON), nil
	}
}

type HoldingsTool struct{}

func (*HoldingsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_holdings",
		mcp.WithDescription("Get holdings for the current user."),
		mcp.WithNumber("from",
			mcp.Description("from is the index from which to show the holdings. (not required unless you have a large payload issue)"),
		),
		mcp.WithNumber("limit",
			mcp.Description("limit is the maximum number of holdings to show. (not required unless you have a large payload issue)"),
		),
	)
}

func (*HoldingsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting holdings", err)
			return nil, err
		}

		holdings, err := kc.Kite.Client.GetHoldings()
		if err != nil {
			log.Println("error getting holdings", err)
			return nil, err
		}

		args := request.Params.Arguments
		// Set defaults for pagination
		from := assertInt(args["from"])
		limit := assertInt(args["limit"])

		// Apply pagination only if there are holdings and limit is specified
		if len(holdings) > 0 && limit > 0 {
			// Ensure from is within bounds
			from = min(max(from, 0), len(holdings))

			// Calculate end index (from + limit) but don't exceed holdings length
			end := min(from+limit, len(holdings))

			// Slice the holdings based on pagination
			holdings = holdings[from:end]
		}

		v, err := json.Marshal(holdings)
		if err != nil {
			log.Println("error marshalling holdings", err)
			return nil, err
		}

		holdingsJSON := string(v)
		return mcp.NewToolResultText(holdingsJSON), nil
	}
}

type PositionsTool struct{}

func (*PositionsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_positions",
		mcp.WithDescription("Get positions"),
	)
}

func (*PositionsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting positions", err)
			return nil, err
		}

		positions, err := kc.Kite.Client.GetPositions()
		if err != nil {
			log.Println("error getting positions", err)
			return nil, err
		}

		v, err := json.Marshal(positions)
		if err != nil {
			log.Println("error marshalling positions", err)
			return nil, err
		}

		positionsJSON := string(v)
		return mcp.NewToolResultText(positionsJSON), nil
	}
}

type TradesTool struct{}

func (*TradesTool) Tool() mcp.Tool {
	return mcp.NewTool("get_trades",
		mcp.WithDescription("Get trades"),
	)
}

func (*TradesTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting trades", err)
			return nil, err
		}

		trades, err := kc.Kite.Client.GetTrades()
		if err != nil {
			log.Println("error getting trades", err)
			return nil, err
		}

		v, err := json.Marshal(trades)
		if err != nil {
			log.Println("error marshalling trades", err)
			return nil, err
		}

		tradesJSON := string(v)
		return mcp.NewToolResultText(tradesJSON), nil
	}
}

type OrdersTool struct{}

func (*OrdersTool) Tool() mcp.Tool {
	return mcp.NewTool("get_orders",
		mcp.WithDescription("Get orders"),
	)
}

func (*OrdersTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		orders, err := kc.Kite.Client.GetOrders()
		if err != nil {
			log.Println("error getting orders", err)
			return nil, err
		}

		v, err := json.Marshal(orders)
		if err != nil {
			log.Println("error marshalling orders", err)
			return nil, err
		}

		ordersJSON := string(v)
		return mcp.NewToolResultText(ordersJSON), nil
	}
}

type GTTOrdersTool struct{}

func (*GTTOrdersTool) Tool() mcp.Tool {
	return mcp.NewTool("get_gtts",
		mcp.WithDescription("Get all active GTT orders"),
	)
}

func (*GTTOrdersTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		gttBook, err := kc.Kite.Client.GetGTTs()
		if err != nil {
			log.Println("error getting GTT book", err)
			return nil, err
		}

		v, err := json.Marshal(gttBook)
		if err != nil {
			log.Println("error marshalling GTT book", err)
			return nil, err
		}

		gttBookJSON := string(v)
		return mcp.NewToolResultText(gttBookJSON), nil
	}
}
