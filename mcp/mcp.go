package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/kc"
)

// TODO: add destructive, openworld and readonly hints where applicable.

// ToolHandler is the function signature for MCP tool handlers using the official SDK
type ToolHandler func(*mcp.CallToolRequest) (*mcp.CallToolResult, error)

// Tool interface defines methods for MCP tools
type Tool interface {
	// Definition returns the tool definition for the official SDK
	Definition() *mcp.Tool
	// Handler returns the tool handler function
	Handler(*kc.Manager) ToolHandler
}

// GetToolNames returns the names of all available tools
func GetToolNames() []string {
	tools := GetAllTools()
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Definition().Name
	}
	return names
}

// GetAllTools returns all available tools for registration
func GetAllTools() []Tool {
	return []Tool{
		&PortfolioTool{},
		&OrdersTool{},
		&GTTTool{},
		&MarketTool{},
		&AlertsTool{},
		&MutualFundsTool{},
		&SessionTool{},
	}
}

// parseExcludedTools parses a comma-separated string of tool names and returns a set of excluded tools.
// This function is exported for testing purposes to ensure tests use the exact same logic as production.
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
// Returns (filteredTools, registeredCount, excludedCount).
// This function is exported for testing purposes to ensure tests use the exact same logic as production.
func filterTools(allTools []Tool, excludedSet map[string]bool) ([]Tool, int, int) {
	filteredTools := make([]Tool, 0, len(allTools))
	excludedCount := 0

	for _, tool := range allTools {
		toolName := tool.Definition().Name
		if excludedSet[toolName] {
			excludedCount++
			continue
		}
		filteredTools = append(filteredTools, tool)
	}

	return filteredTools, len(filteredTools), excludedCount
}

// RegisterTools registers all MCP tools with the official SDK server
func RegisterTools(srv *mcp.Server, manager *kc.Manager, excludedTools string, logger *slog.Logger) {
	// Parse excluded tools list
	excludedSet := parseExcludedTools(excludedTools)

	// Log excluded tools
	for toolName := range excludedSet {
		logger.Info("Excluding tool from registration", "tool", toolName)
	}

	// Filter tools
	allTools := GetAllTools()
	filteredTools, registeredCount, excludedCount := filterTools(allTools, excludedSet)

	// Register filtered tools with official SDK
	for _, tool := range filteredTools {
		def := tool.Definition()
		handler := tool.Handler(manager)
		srv.AddTool(def, wrapHandler(handler))
	}

	logger.Info("Tool registration complete",
		"registered", registeredCount,
		"excluded", excludedCount,
		"total_available", len(allTools))
}

// wrapHandler wraps our ToolHandler to match the official SDK's ToolHandler signature
func wrapHandler(h ToolHandler) mcp.ToolHandler {
	return func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return h(req)
	}
}

// NewToolResultText creates a text result for tool responses
func NewToolResultText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

// NewToolResultError creates an error result for tool responses
func NewToolResultError(errMsg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: errMsg},
		},
		IsError: true,
	}
}

// NewTool creates a new tool definition with the given name and options
func NewTool(name, description string, inputSchema json.RawMessage) *mcp.Tool {
	if inputSchema == nil {
		inputSchema = json.RawMessage(`{"type":"object"}`)
	}
	return &mcp.Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
	}
}
