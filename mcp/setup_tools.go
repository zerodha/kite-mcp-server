package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
)

type LoginTool struct{}

func (*LoginTool) Tool() mcp.Tool {
	return mcp.NewTool("login",
		mcp.WithDescription("Login to Kite API. This tool helps you log in to the Kite API. If you are starting off a new conversation call this tool before hand. Call this if you get a session error."),
	)
}

func (*LoginTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		url := manager.SessionLoginURL(sess.SessionID())

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Show this URL to the user and ask them to login by clicking on it. Wait for the user to complete the login and ask them to let you know to proceed once they have done it in their browser. Try to make sure it is clickable by ideally providing it as a markdown link if your client supports it. URL: %s", url),
				},
			},
		}, nil
	}
}
