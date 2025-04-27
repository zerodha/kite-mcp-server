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
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: profileJSON,
				},
			},
		}, nil
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
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: marginsJSON,
				},
			},
		}, nil
	}
}

type HoldingsTool struct{}

func (*HoldingsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_holdings",
		mcp.WithDescription("Get holdings"),
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

		v, err := json.Marshal(holdings)
		if err != nil {
			log.Println("error marshalling holdings", err)
			return nil, err
		}

		holdingsJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: holdingsJSON,
				},
			},
		}, nil
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
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: positionsJSON,
				},
			},
		}, nil
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
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: tradesJSON,
				},
			},
		}, nil
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
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: ordersJSON,
				},
			},
		}, nil
	}
}
