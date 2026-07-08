package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

// BaseToolHandler provides common functionality for all MCP tools
type BaseToolHandler struct {
	manager *kc.Manager
}

// NewToolHandler creates a new tool handler with the given manager
func NewToolHandler(manager *kc.Manager) *BaseToolHandler {
	return &BaseToolHandler{manager: manager}
}

// GetSessionID extracts the current session ID from the request.
// It prefers the OAuth middleware header and falls back to the MCP session ID.
func GetSessionID(request *mcp.CallToolRequest) string {
	if request == nil {
		return ""
	}
	if request.Extra != nil && request.Extra.Header != nil {
		if sessionID := request.Extra.Header.Get("X-Kite-Session-Id"); sessionID != "" {
			return sessionID
		}
	}
	if request.Session != nil {
		return request.Session.ID()
	}
	return ""
}

// WithKiteClient gets an authenticated Kite client and executes the provided function.
// It handles authentication errors and provides clear instructions to the user.
// The session ID is extracted from the X-Kite-Session-Id header set by OAuth middleware.
func (h *BaseToolHandler) WithKiteClient(request *mcp.CallToolRequest, toolName string, fn func(client *kiteconnect.Client) (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	sessionID := GetSessionID(request)

	h.manager.Logger.Debug("Tool request with session", "tool", toolName, "session_id", sessionID)

	client, err := h.manager.GetAuthenticatedClient(sessionID)
	if err != nil {
		h.manager.Logger.Warn("Failed to get authenticated Kite client", "tool", toolName, "session_id", sessionID, "error", err)
		// Return the specific error message from GetAuthenticatedClient, which guides the user.
		return NewToolResultError(err.Error()), nil
	}

	h.manager.Logger.Debug("Session validated successfully", "tool", toolName, "session_id", sessionID)
	return fn(client)
}

// MarshalResponse marshals data to JSON and returns an MCP text result
func (h *BaseToolHandler) MarshalResponse(data interface{}, toolName string) (*mcp.CallToolResult, error) {
	v, err := json.Marshal(data)
	if err != nil {
		h.manager.Logger.Error("Failed to marshal response", "tool", toolName, "error", err)
		return NewToolResultError("Failed to process response data"), nil
	}

	h.manager.Logger.Debug("Response marshaled successfully", "tool", toolName, "response_size", len(v))
	return NewToolResultText(string(v)), nil
}

// HandleAPICall wraps common API call pattern with error handling and response marshalling
func (h *BaseToolHandler) HandleAPICall(request *mcp.CallToolRequest, toolName string, apiCall func(client *kiteconnect.Client) (interface{}, error)) (*mcp.CallToolResult, error) {
	return h.WithKiteClient(request, toolName, func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
		data, err := apiCall(client)
		if err != nil {
			h.manager.Logger.Error("API call failed", "tool", toolName, "error", err)
			return NewToolResultError(fmt.Sprintf("Failed to execute %s", toolName)), nil
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
		if str, ok := value.(string); ok && str == "" {
			return ValidationError{Parameter: param, Message: "cannot be empty"}
		}
		if arr, ok := value.([]interface{}); ok && len(arr) == 0 {
			return ValidationError{Parameter: param, Message: "cannot be empty"}
		}
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

// Safe assertion helpers remain the same...
func SafeAssertString(v interface{}, fallback string) string {
	if v == nil {
		return fallback
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

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

// Pagination logic remains the same...
type PaginationParams struct {
	From  int
	Limit int
}

func ParsePaginationParams(args map[string]interface{}) PaginationParams {
	return PaginationParams{
		From:  SafeAssertInt(args["from"], 0),
		Limit: SafeAssertInt(args["limit"], 0),
	}
}

func ApplyPagination[T any](data []T, params PaginationParams) []T {
	if len(data) == 0 {
		return data
	}
	from := min(max(params.From, 0), len(data))
	if params.Limit <= 0 {
		return data[from:]
	}
	end := min(from+params.Limit, len(data))
	return data[from:end]
}

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

func CreatePaginatedResponse(originalData interface{}, paginatedData interface{}, params PaginationParams, originalLength int) *PaginatedResponse {
	response := &PaginatedResponse{
		Data: paginatedData,
	}
	response.Pagination.From = params.From
	response.Pagination.Limit = params.Limit
	response.Pagination.Total = originalLength
	returnedCount := 0
	if paginatedData != nil {
		switch data := paginatedData.(type) {
		case []interface{}:
			returnedCount = len(data)
		default:
			from := max(0, min(params.From, originalLength))
			if params.Limit > 0 {
				returnedCount = min(params.Limit, max(0, originalLength-from))
			} else {
				returnedCount = max(0, originalLength-from)
			}
		}
	} else {
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

// GetArguments extracts arguments from the request as a map
func GetArguments(request *mcp.CallToolRequest) map[string]interface{} {
	if request.Params == nil || request.Params.Arguments == nil {
		return make(map[string]interface{})
	}
	var args map[string]interface{}
	if err := json.Unmarshal(request.Params.Arguments, &args); err != nil {
		return make(map[string]interface{})
	}
	return args
}

// SimpleToolHandler creates a handler for simple API calls
func SimpleToolHandler(manager *kc.Manager, toolName string, apiCall func(client *kiteconnect.Client) (interface{}, error)) ToolHandler {
	handler := NewToolHandler(manager)
	return func(request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handler.HandleAPICall(request, toolName, apiCall)
	}
}

// PaginatedToolHandler creates a handler for paginated API calls
func PaginatedToolHandler[T any](manager *kc.Manager, toolName string, apiCall func(client *kiteconnect.Client) ([]T, error)) ToolHandler {
	handler := NewToolHandler(manager)
	return func(request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handler.WithKiteClient(request, toolName, func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			data, err := apiCall(client)
			if err != nil {
				handler.manager.Logger.Error("API call failed", "tool", toolName, "error", err)
				return NewToolResultError(fmt.Sprintf("Failed to execute %s", toolName)), nil
			}
			args := GetArguments(request)
			params := ParsePaginationParams(args)
			originalLength := len(data)
			paginatedData := ApplyPagination(data, params)
			var responseData interface{}
			if params.Limit > 0 {
				responseData = CreatePaginatedResponse(data, paginatedData, params, originalLength)
			} else {
				responseData = paginatedData
			}
			return handler.MarshalResponse(responseData, toolName)
		})
	}
}
