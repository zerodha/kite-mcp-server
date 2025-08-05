package mcp

import (
	"context"
	"log/slog"
	"strings"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
)

// TODO: add destructive, openworld and readonly hints where applicable.

type Tool interface {
	Tool() gomcp.Tool
	Handler(*kc.Manager) server.ToolHandlerFunc
}

// GetAllTools returns all available tools for registration
func GetAllTools() []Tool {
	return []Tool{
		// Tools for setting up the client
		&LoginTool{},

		// Tools that get data from Kite Connect
		&ProfileTool{},
		&MarginsTool{},
		&HoldingsTool{},
		&PositionsTool{},
		&TradesTool{},
		&OrdersTool{},
		&OrderHistoryTool{},
		&OrderTradesTool{},
		&GTTOrdersTool{},
		&MFHoldingsTool{},

		// Tools for market data
		&QuotesTool{},
		&InstrumentsSearchTool{},
		&HistoricalDataTool{},
		&LTPTool{},
		&OHLCTool{},

		// Tools that post data to Kite Connect
		&PlaceOrderTool{},
		&ModifyOrderTool{},
		&CancelOrderTool{},
		&PlaceGTTOrderTool{},
		&ModifyGTTOrderTool{},
		&DeleteGTTOrderTool{},
	}
}

// parseExcludedTools parses a comma-separated string of tool names and returns a set of excluded tools.
func parseExcludedTools(excludedTools string) map[string]bool {
	excludedSet := make(map[string]bool)
	if excludedTools != "" {
		excluded := strings.Split(excludedTools, ",")
		for _, toolName := range excluded {
			toolName = strings.TrimSpace(toolName)
			if toolName != "" {
				excludedSet[toolName] = true
			}
		}
	}
	return excludedSet
}

// filterTools returns tools that are not in the excluded set, along with counts.
func filterTools(allTools []Tool, excludedSet map[string]bool) ([]Tool, int, int) {
	filteredTools := make([]Tool, 0, len(allTools))
	excludedCount := 0

	for _, tool := range allTools {
		toolName := tool.Tool().Name
		if excludedSet[toolName] {
			excludedCount++
			continue
		}
		filteredTools = append(filteredTools, tool)
	}

	return filteredTools, len(filteredTools), excludedCount
}

func RegisterTools(srv *server.MCPServer, manager *kc.Manager, excludedTools string, logger *slog.Logger) {
	// Parse excluded tools list
	excludedSet := parseExcludedTools(excludedTools)

	// Log excluded tools
	for toolName := range excludedSet {
		logger.Info("Excluding tool from registration", "tool", toolName)
	}

	// Filter tools
	allTools := GetAllTools()
	filteredTools, registeredCount, excludedCount := filterTools(allTools, excludedSet)

	// Register filtered tools with session-aware login tool handling
	for _, tool := range filteredTools {
		if tool.Tool().Name == "login" {
			// Register login tool with session-aware handler that can reject calls
			srv.AddTool(tool.Tool(), createSessionAwareLoginHandler(tool.Handler(manager), manager, logger))
		} else {
			srv.AddTool(tool.Tool(), tool.Handler(manager))
		}
	}

	logger.Info("Tool registration complete",
		"registered", registeredCount,
		"excluded", excludedCount,
		"total_available", len(allTools))
}

// createSessionAwareLoginHandler creates a handler that rejects login tool calls when user is authenticated
func createSessionAwareLoginHandler(originalHandler server.ToolHandlerFunc, manager *kc.Manager, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		mcpSession := server.ClientSessionFromContext(ctx)
		sessionID := mcpSession.SessionID()

		// Check if user has valid session credentials
		if _, err := manager.GetAuthenticatedClient(sessionID); err == nil {
			// User is authenticated - reject the tool call
			logger.Info("Login tool call rejected - user has valid session", "session_id", sessionID)
			return gomcp.NewToolResultError("Tool unavailable: User is already authenticated. The login tool is disabled for authenticated sessions."), nil
		}

		// User is not authenticated, proceed with original handler
		return originalHandler(ctx, request)
	}
}
