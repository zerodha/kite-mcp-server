package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/agentic"
	"github.com/zerodha/kite-mcp-server/kc"
)

type fakeMCPClientSession struct {
	id string
	ch chan gomcp.JSONRPCNotification
}

func (f fakeMCPClientSession) SessionID() string { return f.id }
func (f fakeMCPClientSession) Initialize()       {}
func (f fakeMCPClientSession) Initialized() bool { return true }
func (f fakeMCPClientSession) NotificationChannel() chan<- gomcp.JSONRPCNotification {
	if f.ch == nil {
		f.ch = make(chan gomcp.JSONRPCNotification)
	}
	return f.ch
}

func toolContext(t *testing.T, sessionID string) context.Context {
	t.Helper()
	srv := server.NewMCPServer("test", "v0")
	return srv.WithContext(context.Background(), fakeMCPClientSession{id: sessionID})
}

func callToolRequest(args map[string]any) gomcp.CallToolRequest {
	return gomcp.CallToolRequest{
		Params: gomcp.CallToolParams{
			Arguments: args,
		},
	}
}

func toolResultText(t *testing.T, result *gomcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	text, ok := gomcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	return text.Text
}

func newAgenticToolsTestManager(t *testing.T) *kc.Manager {
	t.Helper()
	manager, err := kc.New(kc.Config{
		APIKey:    "test_key",
		APISecret: "test_secret",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	manager.Agentic = agentic.NewStore()
	return manager
}

func TestAgenticToolsRegistered(t *testing.T) {
	names := make(map[string]bool)
	for _, tool := range GetAllTools() {
		names[tool.Tool().Name] = true
	}
	assert.True(t, names["setup_agent_account"])
	assert.True(t, names["get_agent_account"])
	assert.True(t, names["disconnect_agent"])
}

func TestSetupAgentAccountTool_Success(t *testing.T) {
	const sessionID = "kitemcp-00000000-0000-4000-8000-000000000001"
	manager := newAgenticToolsTestManager(t)
	ctx := toolContext(t, sessionID)

	result, err := (&SetupAgentAccountTool{}).Handler(manager)(ctx, callToolRequest(map[string]any{
		"budget_inr": 25000.0,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	body := toolResultText(t, result)
	assert.Contains(t, body, "25000")
	assert.Contains(t, body, "Agentic account ready")

	acct := manager.Agentic.Get(sessionID)
	assert.True(t, acct.Active)
	assert.Equal(t, 25000.0, acct.BudgetINR)
}

func TestSetupAgentAccountTool_RequiresAgenticEnabled(t *testing.T) {
	manager := newAgenticToolsTestManager(t)
	manager.Agentic = nil
	ctx := toolContext(t, "kitemcp-00000000-0000-4000-8000-000000000002")

	result, err := (&SetupAgentAccountTool{}).Handler(manager)(ctx, callToolRequest(map[string]any{
		"budget_inr": 1000.0,
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, toolResultText(t, result), "not enabled")
}

func TestSetupAgentAccountTool_RequiresBudget(t *testing.T) {
	manager := newAgenticToolsTestManager(t)
	ctx := toolContext(t, "kitemcp-00000000-0000-4000-8000-000000000003")

	result, err := (&SetupAgentAccountTool{}).Handler(manager)(ctx, callToolRequest(map[string]any{}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestGetAgentAccountTool_ActiveAccount(t *testing.T) {
	const sessionID = "kitemcp-00000000-0000-4000-8000-000000000004"
	manager := newAgenticToolsTestManager(t)
	manager.Agentic.Setup(sessionID, 8000)
	manager.Agentic.RecordSpend(sessionID, 1500)
	ctx := toolContext(t, sessionID)

	result, err := (&GetAgentAccountTool{}).Handler(manager)(ctx, callToolRequest(nil))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var acct agentic.Account
	require.NoError(t, json.Unmarshal([]byte(toolResultText(t, result)), &acct))
	assert.True(t, acct.Active)
	assert.Equal(t, 8000.0, acct.BudgetINR)
	assert.Equal(t, 1500.0, acct.SpentINR)
	assert.Equal(t, 6500.0, acct.RemainingINR)
}

func TestDisconnectAgentTool_ClearsBudget(t *testing.T) {
	const sessionID = "kitemcp-00000000-0000-4000-8000-000000000005"
	manager := newAgenticToolsTestManager(t)
	manager.Agentic.Setup(sessionID, 5000)
	ctx := toolContext(t, sessionID)

	result, err := (&DisconnectAgentTool{}).Handler(manager)(ctx, callToolRequest(nil))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, toolResultText(t, result), "disconnected")
	assert.False(t, manager.Agentic.Get(sessionID).Active)
}
