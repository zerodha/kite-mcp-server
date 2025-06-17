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
		mcp.WithDescription("Login to Kite or refresh an expired 24-hour Kite session. Returns a link that the user should click to authorize access. Present this as a markdown link for easy clicking."),
	)
}

func (*LoginTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mcpSession := server.ClientSessionFromContext(ctx)
		sessionID := mcpSession.SessionID()
		manager.Logger.Info("Login tool called", "session_id", sessionID)

		// Check if the user is already logged in and has a valid token.
		if client, err := manager.GetAuthenticatedClient(sessionID); err == nil {
			profile, err := client.GetUserProfile()
			if err == nil {
				manager.Logger.Info("User is already logged in with a valid Kite session.", "session_id", sessionID, "user", profile.UserName)
				return mcp.NewToolResultText(fmt.Sprintf("You are already logged in as %s. There is no need to log in again.", profile.UserName)), nil
			}
		}

		// If not, generate a login URL for them. This works for both first-time login and re-login.
		url, err := manager.GenerateLoginURL(sessionID)
		if err != nil {
			manager.Logger.Error("Error generating Kite login URL", "session_id", sessionID, "error", err)
			return mcp.NewToolResultError("Failed to generate Kite login URL"), nil
		}

		manager.Logger.Info("Successfully generated Kite login URL", "session_id", sessionID)
		return mcp.NewToolResultText(fmt.Sprintf("Please log in to Kite by clicking this link: [Login to Kite](%s)\n\nAfter completing the login in your browser, you can continue with your request.", url)), nil
	}
}
