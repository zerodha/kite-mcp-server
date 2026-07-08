package mcp

import (
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/kc"
)

type SessionTool struct{}

var sessionSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"mode": {
			"type": "string",
			"description": "Operation mode: status=current session state, logout=invalidate current session",
			"enum": ["status", "logout"]
		}
	},
	"required": ["mode"]
}`)

func (*SessionTool) Definition() *mcp.Tool {
	return NewTool("session",
		"Inspect or invalidate the current authenticated session. Use mode=status to check session state and mode=logout to invalidate the current session.",
		sessionSchema,
	)
}

func (*SessionTool) Handler(manager *kc.Manager) ToolHandler {
	handler := NewToolHandler(manager)
	return func(request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := GetArguments(request)
		mode := SafeAssertString(args["mode"], "")
		sessionID := GetSessionID(request)
		if sessionID == "" {
			return NewToolResultError("unable to determine current session"), nil
		}

		switch mode {
		case "status":
			return handleSessionStatus(handler, sessionID)
		case "logout":
			return handleSessionLogout(handler, sessionID)
		default:
			return NewToolResultError("Invalid mode. Must be one of: status, logout"), nil
		}
	}
}

func handleSessionStatus(handler *BaseToolHandler, sessionID string) (*mcp.CallToolResult, error) {
	session, err := handler.manager.SessionManager().Get(sessionID)
	if err != nil {
		status := map[string]interface{}{
			"session_id":    sessionID,
			"authenticated": false,
			"terminated":    true,
			"status":        "logged_out",
		}
		return handler.MarshalResponse(status, "session_status")
	}

	now := time.Now()
	kiteExpiresAt := ""
	authenticated := false
	kiteSessionExpired := false
	if session.Credentials != nil {
		authenticated = now.Before(session.Credentials.ExpiresAt)
		kiteSessionExpired = now.After(session.Credentials.ExpiresAt)
		kiteExpiresAt = session.Credentials.ExpiresAt.Format(time.RFC3339)
	}

	status := map[string]interface{}{
		"session_id":              session.ID,
		"authenticated":           authenticated,
		"terminated":              session.Terminated,
		"status":                  sessionStatusLabel(session, authenticated),
		"created_at":              session.CreatedAt.Format(time.RFC3339),
		"expires_at":              session.ExpiresAt.Format(time.RFC3339),
		"kite_session_expires_at": kiteExpiresAt,
		"kite_session_expired":    kiteSessionExpired,
	}
	return handler.MarshalResponse(status, "session_status")
}

func handleSessionLogout(handler *BaseToolHandler, sessionID string) (*mcp.CallToolResult, error) {
	_, err := handler.manager.SessionManager().Terminate(sessionID)
	if err != nil {
		if err.Error() == "session ID not found" {
			return handler.MarshalResponse(map[string]interface{}{
				"success":    true,
				"session_id": sessionID,
				"status":     "already_logged_out",
			}, "session_logout")
		}
		return NewToolResultError("Failed to logout current session"), nil
	}

	return handler.MarshalResponse(map[string]interface{}{
		"success":    true,
		"session_id": sessionID,
		"status":     "logged_out",
	}, "session_logout")
}

func sessionStatusLabel(session *kc.Session, authenticated bool) string {
	if session.Terminated {
		return "terminated"
	}
	if session.Credentials == nil {
		return "not_authenticated"
	}
	if !authenticated {
		return "expired"
	}
	return "authenticated"
}
