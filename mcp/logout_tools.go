package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
)

type LogoutTool struct{}

func (*LogoutTool) Tool() mcp.Tool {
	return mcp.NewTool("logout",
		mcp.WithDescription("Logout from the current Kite session. This invalidates the access token and clears session data, allowing you to log in with a different account. Call this between multi-account operations."),
	)
}

func (*LogoutTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler := NewToolHandler(manager)
		handler.trackToolCall(ctx, "logout")

		mcpClientSession := server.ClientSessionFromContext(ctx)
		mcpSessionID := mcpClientSession.SessionID()
		manager.Logger.Info("Logout tool called", "session_id", mcpSessionID)

		// Try to get the current user profile before clearing, for a friendly message
		var userName string
		kiteSession, _, err := manager.GetOrCreateSession(mcpSessionID)
		if err == nil && kiteSession != nil {
			profile, err := kiteSession.Kite.Client.GetUserProfile()
			if err == nil {
				userName = profile.UserName
			}
		}

		// Clear session data (invalidates token) but keep MCP session alive for re-login
		if err := manager.ClearSessionData(mcpSessionID); err != nil {
			manager.Logger.Error("Failed to clear session data during logout", "session_id", mcpSessionID, "error", err)
			handler.trackToolError(ctx, "logout", "clear_error")
			return mcp.NewToolResultError("Failed to logout. Please try again."), nil
		}

		manager.Logger.Info("COMPLIANCE: User logout completed successfully",
			"event", "user_logout_success",
			"session_id", mcpSessionID,
			"user_name", userName,
		)

		msg := "Successfully logged out."
		if userName != "" {
			msg = fmt.Sprintf("Successfully logged out user %s.", userName)
		}
		msg += " You can now log in with a different account using the login tool."

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: msg,
				},
			},
		}, nil
	}
}
