package mcp

import (
	"context"
	"encoding/json"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
)

type MFHoldingsTool struct{}

func (*MFHoldingsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_mf_holdings",
		mcp.WithDescription("Get all mutual fund holdings"),
	)
}

func (*MFHoldingsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		holdings, err := kc.Kite.Client.GetMFHoldings()
		if err != nil {
			log.Println("error getting mutual fund holdings", err)
			return nil, err
		}

		v, err := json.Marshal(holdings)
		if err != nil {
			log.Println("error marshalling mutual fund holdings", err)
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
