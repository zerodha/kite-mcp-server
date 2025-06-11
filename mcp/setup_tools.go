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
		mcp.WithDescription("Login to Kite API. This tool helps you log in to the Kite API. If you are starting off a new conversation call this tool before hand. Call this if you get a session error. Returns a link that the user should click to authorize access, present as markdown if your client supports so that they can click it easily when rendered."),
	)
}

func (*LoginTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Get MCP client session from context
		mcpClientSession := server.ClientSessionFromContext(ctx)

		// Extract MCP session ID
		mcpSessionID := mcpClientSession.SessionID()
		manager.Logger.Info("Login tool called", "session_id", mcpSessionID)

		// Get or create a Kite session for this MCP session
		kiteSession, isNew, err := manager.GetOrCreateSession(mcpSessionID)
		if err != nil {
			manager.Logger.Error("Failed to get or create Kite session", "session_id", mcpSessionID, "error", err)
			return mcp.NewToolResultError("Failed to get or create Kite session"), nil
		}

		if !isNew {
			// We have an existing session, verify it works by getting the profile
			manager.Logger.Debug("Found existing Kite session, verifying with profile check", "session_id", mcpSessionID)
			profile, err := kiteSession.Kite.Client.GetUserProfile()
			if err != nil {
				manager.Logger.Warn("Kite profile check failed, clearing session data", "session_id", mcpSessionID, "error", err)
				// If we are still getting an error, lets clear session data and recreate
				if clearErr := manager.ClearSessionData(mcpSessionID); clearErr != nil {
					manager.Logger.Error("Failed to clear session data", "session_id", mcpSessionID, "error", clearErr)
					return mcp.NewToolResultError("Failed to clear session data"), nil
				}

				// Create a new session
				_, _, err = manager.GetOrCreateSession(mcpSessionID)
				if err != nil {
					manager.Logger.Error("Failed to create new Kite session", "session_id", mcpSessionID, "error", err)
					return mcp.NewToolResultError("Failed to create new Kite session"), nil
				}
			} else {
				manager.Logger.Info("Kite profile check successful", "session_id", mcpSessionID, "user", profile.UserName)
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.TextContent{
							Type: "text",
							Text: fmt.Sprintf("You are already logged in as %s", profile.UserName),
						},
					},
				}, nil
			}
		}

		// Proceed with Kite login URL generation using the MCP session
		url, err := manager.SessionLoginURL(mcpSessionID)
		if err != nil {
			manager.Logger.Error("Error generating Kite login URL", "session_id", mcpSessionID, "error", err)
			return mcp.NewToolResultError("Failed to generate Kite login URL"), nil
		}

		manager.Logger.Info("Successfully generated Kite login URL", "session_id", mcpSessionID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Please login to Kite by clicking this link: [Login to Kite](%s)\n\nIf your client supports clickable links, you can render and present it and ask them to click the link above. Otherwise, display the URL and ask them to copy and paste it into their browser: %s\n\nAfter completing the login in your browser, let me know and I'll continue with your request.", url, url),
				},
			},
		}, nil
	}
}
