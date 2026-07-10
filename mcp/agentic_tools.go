package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
)

type SetupAgentAccountTool struct{}

func (*SetupAgentAccountTool) Tool() gomcp.Tool {
	return gomcp.NewTool("setup_agent_account",
		gomcp.WithDescription("Create an agentic trading account for this agent session with a dedicated INR budget. Call after login and before placing orders. Orders via place_order are blocked when they exceed the remaining budget."),
		gomcp.WithNumber("budget_inr",
			gomcp.Description("Maximum INR the agent may deploy via place_order in this session"),
			gomcp.Required(),
			gomcp.Min(1),
		),
	)
}

func (*SetupAgentAccountTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "setup_agent_account")
		args := request.GetArguments()

		if err := ValidateRequired(args, "budget_inr"); err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}

		if manager.Agentic == nil {
			return gomcp.NewToolResultError("Agentic accounts are not enabled on this server"), nil
		}

		budget := SafeAssertFloat64(args["budget_inr"], 0)
		if budget <= 0 {
			return gomcp.NewToolResultError("budget_inr must be greater than 0"), nil
		}

		sess := server.ClientSessionFromContext(ctx)
		account := manager.Agentic.Setup(sess.SessionID(), budget)

		return handler.MarshalResponse(map[string]any{
			"message": fmt.Sprintf("Agentic account ready with ₹%.2f budget. Trades are capped to this amount for this agent session.", budget),
			"account": account,
		}, "setup_agent_account")
	}
}

type GetAgentAccountTool struct{}

func (*GetAgentAccountTool) Tool() gomcp.Tool {
	return gomcp.NewTool("get_agent_account",
		gomcp.WithDescription("Get the agentic account status: budget, spent, and remaining INR for this agent session."),
	)
}

func (*GetAgentAccountTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "get_agent_account")

		if manager.Agentic == nil {
			return gomcp.NewToolResultError("Agentic accounts are not enabled on this server"), nil
		}

		sess := server.ClientSessionFromContext(ctx)
		account := manager.Agentic.Get(sess.SessionID())

		return handler.MarshalResponse(account, "get_agent_account")
	}
}

type DisconnectAgentTool struct{}

func (*DisconnectAgentTool) Tool() gomcp.Tool {
	return gomcp.NewTool("disconnect_agent",
		gomcp.WithDescription("Disconnect the agent: clears the agentic budget and logs out the linked Kite session. The agent cannot trade until the user logs in again via the login tool."),
	)
}

func (*DisconnectAgentTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "disconnect_agent")

		sess := server.ClientSessionFromContext(ctx)
		sessionID := sess.SessionID()

		if manager.Agentic != nil {
			manager.Agentic.Remove(sessionID)
		}

		if err := manager.ClearSessionData(sessionID); err != nil {
			handler.manager.Logger.Warn("disconnect_agent: session data clear", "session_id", sessionID, "error", err)
		}

		return handler.MarshalResponse(map[string]string{
			"status":  "disconnected",
			"message": "Agent disconnected. Kite login and agentic budget cleared for this session.",
		}, "disconnect_agent")
	}
}
