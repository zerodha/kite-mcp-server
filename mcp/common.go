package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
)

// Context key for session type
type contextKey string

const (
	sessionTypeKey contextKey = "session_type"
)

// Session type constants
const (
	SessionTypeSSE     = "sse"
	SessionTypeMCP     = "mcp"
	SessionTypeStdio   = "stdio"
	SessionTypeUnknown = "unknown"
)

// WithSessionType adds session type to context
func WithSessionType(ctx context.Context, sessionType string) context.Context {
	return context.WithValue(ctx, sessionTypeKey, sessionType)
}

// SessionTypeFromContext extracts session type from context
func SessionTypeFromContext(ctx context.Context) string {
	if sessionType, ok := ctx.Value(sessionTypeKey).(string); ok {
		return sessionType
	}
	return SessionTypeUnknown // default fallback for undetermined sessions
}

// ToolHandler provides common functionality for all MCP tools
type ToolHandler struct {
	manager *kc.Manager
}

// NewToolHandler creates a new tool handler with the given manager
func NewToolHandler(manager *kc.Manager) *ToolHandler {
	return &ToolHandler{manager: manager}
}

// trackToolCall increments the daily tool usage counter with optional context for session type
func (h *ToolHandler) trackToolCall(ctx context.Context, toolName string) {
	if h.manager.HasMetrics() {
		sessionType := SessionTypeFromContext(ctx)
		labels := map[string]string{
			"tool":         toolName,
			"session_type": sessionType,
		}
		h.manager.IncrementDailyMetricWithLabels("tool_calls", labels)
	}
}

// trackToolError increments the daily tool error counter with error type and optional context for session type
func (h *ToolHandler) trackToolError(ctx context.Context, toolName, errorType string) {
	if h.manager.HasMetrics() {
		sessionType := SessionTypeFromContext(ctx)
		labels := map[string]string{
			"tool":         toolName,
			"error_type":   errorType,
			"session_type": sessionType,
		}
		h.manager.IncrementDailyMetricWithLabels("tool_errors", labels)
	}
}

// WithSession validates session and executes the provided function with a valid Kite session
// This eliminates the TOCTOU race condition by consolidating session validation and usage
func (h *ToolHandler) WithSession(ctx context.Context, toolName string, fn func(*kc.KiteSessionData) (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	sess := server.ClientSessionFromContext(ctx)
	sessionID := sess.SessionID()

	h.manager.Logger.Debug("Tool request with session", "tool", toolName, "session_id", sessionID)

	kiteSession, isNew, err := h.manager.GetOrCreateSession(sessionID)
	if err != nil {
		h.manager.Logger.Error("Failed to establish session", "tool", toolName, "session_id", sessionID, "error", err)
		h.trackToolError(ctx, toolName, "session_error")
		return mcp.NewToolResultError("Failed to establish a session. Please try again."), nil
	}

	if isNew {
		h.manager.Logger.Info("New session created, login required", "tool", toolName, "session_id", sessionID)
		h.trackToolError(ctx, toolName, "auth_required")
		return mcp.NewToolResultError("Please log in first using the login tool"), nil
	}

	h.manager.Logger.Debug("Session validated successfully", "tool", toolName, "session_id", sessionID)
	return fn(kiteSession)
}

// MarshalResponse marshals data to JSON and returns an MCP text result
func (h *ToolHandler) MarshalResponse(data interface{}, toolName string) (*mcp.CallToolResult, error) {
	v, err := json.Marshal(data)
	if err != nil {
		h.manager.Logger.Error("Failed to marshal response", "tool", toolName, "error", err)
		return mcp.NewToolResultError("Failed to process response data"), nil
	}

	h.manager.Logger.Debug("Response marshaled successfully", "tool", toolName, "response_size", len(v))
	return mcp.NewToolResultText(string(v)), nil
}

// HandleAPICall wraps common API call pattern with error handling and response marshalling
func (h *ToolHandler) HandleAPICall(ctx context.Context, toolName string, apiCall func(*kc.KiteSessionData) (interface{}, error)) (*mcp.CallToolResult, error) {
	return h.WithSession(ctx, toolName, func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
		data, err := apiCall(session)
		if err != nil {
			h.manager.Logger.Error("API call failed", "tool", toolName, "error", err)
			return mcp.NewToolResultError(fmt.Sprintf("Failed to execute %s", toolName)), nil
		}

		return h.MarshalResponse(data, toolName)
	})
}

// ValidationError represents a parameter validation error
type ValidationError struct {
	Parameter string
	Message   string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("parameter '%s': %s", e.Parameter, e.Message)
}

// ValidateRequired checks if required parameters are present and non-empty
func ValidateRequired(args map[string]interface{}, required ...string) error {
	for _, param := range required {
		value := args[param]
		if value == nil {
			return ValidationError{Parameter: param, Message: "is required"}
		}

		// Check for empty strings
		if str, ok := value.(string); ok && str == "" {
			return ValidationError{Parameter: param, Message: "cannot be empty"}
		}

		// Check for empty arrays/slices using reflection
		if arr, ok := value.([]interface{}); ok && len(arr) == 0 {
			return ValidationError{Parameter: param, Message: "cannot be empty"}
		}

		// Check for other slice types
		switch v := value.(type) {
		case []string:
			if len(v) == 0 {
				return ValidationError{Parameter: param, Message: "cannot be empty"}
			}
		case []int:
			if len(v) == 0 {
				return ValidationError{Parameter: param, Message: "cannot be empty"}
			}
		}
	}
	return nil
}

// SafeAssertString safely converts interface{} to string with fallback
func SafeAssertString(v interface{}, fallback string) string {
	if v == nil {
		return fallback
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// SafeAssertInt safely converts interface{} to int with fallback
func SafeAssertInt(v interface{}, fallback int) int {
	if v == nil {
		return fallback
	}
	if i, ok := v.(int); ok {
		return i
	}
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return fallback
}

// SafeAssertFloat64 safely converts interface{} to float64 with fallback
func SafeAssertFloat64(v interface{}, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	if f, ok := v.(float64); ok {
		return f
	}
	if i, ok := v.(int); ok {
		return float64(i)
	}
	return fallback
}

// SafeAssertBool safely converts interface{} to bool with fallback
func SafeAssertBool(v interface{}, fallback bool) bool {
	if v == nil {
		return fallback
	}
	if b, ok := v.(bool); ok {
		return b
	}
	if s, ok := v.(string); ok {
		switch s {
		case "true", "True", "TRUE", "1", "yes", "Yes", "YES", "on", "On", "ON":
			return true
		case "false", "False", "FALSE", "0", "no", "No", "NO", "off", "Off", "OFF":
			return false
		}
	}
	return fallback
}

// SafeAssertStringArray safely converts interface{} to []string with fallback
func SafeAssertStringArray(v interface{}) []string {
	if v == nil {
		return nil
	}

	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}

	result := make([]string, 0, len(arr))
	for _, item := range arr {
		str := SafeAssertString(item, "")
		if str != "" {
			result = append(result, str)
		}
	}
	return result
}

// PaginationParams holds pagination parameters
type PaginationParams struct {
	From  int
	Limit int
}

// ParsePaginationParams extracts pagination parameters from arguments
func ParsePaginationParams(args map[string]interface{}) PaginationParams {
	return PaginationParams{
		From:  SafeAssertInt(args["from"], 0),
		Limit: SafeAssertInt(args["limit"], 0),
	}
}

// ApplyPagination applies pagination to any slice using reflection-like approach
func ApplyPagination[T any](data []T, params PaginationParams) []T {
	// If empty data, return empty slice
	if len(data) == 0 {
		return data
	}

	// Ensure from is within bounds
	from := min(max(params.From, 0), len(data))

	// If no limit specified, return from offset to end
	if params.Limit <= 0 {
		return data[from:]
	}

	// Calculate end index (from + limit) but don't exceed data length
	end := min(from+params.Limit, len(data))

	// Return paginated slice
	return data[from:end]
}

// PaginatedResponse wraps a response with pagination metadata
type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Pagination struct {
		From     int  `json:"from"`
		Limit    int  `json:"limit"`
		Total    int  `json:"total"`
		HasMore  bool `json:"has_more"`
		Returned int  `json:"returned"`
	} `json:"pagination"`
}

// CreatePaginatedResponse creates a paginated response with metadata
func CreatePaginatedResponse(originalData interface{}, paginatedData interface{}, params PaginationParams, originalLength int) *PaginatedResponse {
	response := &PaginatedResponse{
		Data: paginatedData,
	}

	response.Pagination.From = params.From
	response.Pagination.Limit = params.Limit
	response.Pagination.Total = originalLength

	// Calculate returned count based on actual paginated data
	returnedCount := 0
	if paginatedData != nil {
		switch data := paginatedData.(type) {
		case []interface{}:
			returnedCount = len(data)
		default:
			// For other types, calculate based on parameters with bounds checking
			from := max(0, min(params.From, originalLength))
			if params.Limit > 0 {
				returnedCount = min(params.Limit, max(0, originalLength-from))
			} else {
				returnedCount = max(0, originalLength-from)
			}
		}
	} else {
		// Handle nil paginated data by calculating from parameters
		from := max(0, min(params.From, originalLength))
		if params.Limit > 0 {
			returnedCount = min(params.Limit, max(0, originalLength-from))
		} else {
			returnedCount = max(0, originalLength-from)
		}
	}

	response.Pagination.Returned = returnedCount
	response.Pagination.HasMore = params.From+returnedCount < originalLength

	return response
}

// SimpleToolHandler creates a handler function for simple GET endpoints
func SimpleToolHandler(manager *kc.Manager, toolName string, apiCall func(*kc.KiteSessionData) (interface{}, error)) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Track the tool call at the handler level
		handler.trackToolCall(ctx, toolName)
		result, err := handler.HandleAPICall(ctx, toolName, apiCall)
		if err != nil {
			handler.trackToolError(ctx, toolName, "execution_error")
		} else if result != nil && result.IsError {
			handler.trackToolError(ctx, toolName, "api_error")
		}
		return result, err
	}
}

// PaginatedToolHandler creates a handler function for endpoints that support pagination
func PaginatedToolHandler[T any](manager *kc.Manager, toolName string, apiCall func(*kc.KiteSessionData) ([]T, error)) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Track the tool call at the handler level
		handler.trackToolCall(ctx, toolName)
		result, err := handler.WithSession(ctx, toolName, func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
			// Get the data
			data, err := apiCall(session)
			if err != nil {
				handler.manager.Logger.Error("API call failed", "tool", toolName, "error", err)
				handler.trackToolError(ctx, toolName, "api_error")
				return mcp.NewToolResultError(fmt.Sprintf("Failed to execute %s", toolName)), nil
			}

			// Parse pagination parameters
			args := request.GetArguments()
			params := ParsePaginationParams(args)

			// Apply pagination if limit is specified
			originalLength := len(data)
			paginatedData := ApplyPagination(data, params)

			// Create response with pagination metadata if pagination was applied
			var responseData interface{}
			if params.Limit > 0 {
				responseData = CreatePaginatedResponse(data, paginatedData, params, originalLength)
			} else {
				responseData = paginatedData
			}

			return handler.MarshalResponse(responseData, toolName)
		})

		if err != nil {
			handler.trackToolError(ctx, toolName, "execution_error")
		} else if result != nil && result.IsError {
			handler.trackToolError(ctx, toolName, "api_error")
		}
		return result, err
	}
}
