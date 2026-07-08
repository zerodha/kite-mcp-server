package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

type AlertsTool struct{}

var alertsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"mode": {
			"type": "string",
			"description": "Operation mode: get=retrieve alerts, create=new alert, modify=update alert, delete=remove alerts",
			"enum": ["get", "create", "modify", "delete"]
		},
		"uuids": {
			"type": "array",
			"description": "Array of alert UUIDs (for get/modify/delete modes). Single UUID for specific alert, multiple for batch operations",
			"items": {
				"type": "string"
			}
		},
		"status": {
			"type": "string",
			"description": "Filter alerts by status (only for get mode)",
			"enum": ["enabled", "disabled", "deleted"]
		},
		"type": {
			"type": "string",
			"description": "Filter alerts by type (only for get mode)",
			"enum": ["simple", "ato"]
		},
		"history": {
			"type": "boolean",
			"description": "Include alert history (only for get mode, default: false)"
		},
		"name": {
			"type": "string",
			"description": "Alert name (required for create/modify modes)"
		},
		"alert_type": {
			"type": "string",
			"description": "Alert type: simple or ato (required for create/modify modes)",
			"enum": ["simple", "ato"]
		},
		"lhs_exchange": {
			"type": "string",
			"description": "Exchange for left-hand side instrument (required for create/modify modes)"
		},
		"lhs_tradingsymbol": {
			"type": "string",
			"description": "Trading symbol for left-hand side instrument (required for create/modify modes)"
		},
		"lhs_attribute": {
			"type": "string",
			"description": "Attribute for left-hand side (e.g., LastTradedPrice) (required for create/modify modes)"
		},
		"operator": {
			"type": "string",
			"description": "Comparison operator: <=, >=, <, >, == (required for create/modify modes)",
			"enum": ["<=", ">=", "<", ">", "=="]
		},
		"rhs_type": {
			"type": "string",
			"description": "Right-hand side type: constant or instrument (required for create/modify modes)",
			"enum": ["constant", "instrument"]
		},
		"rhs_constant": {
			"type": "number",
			"description": "Constant value for right-hand side (required if rhs_type=constant)"
		},
		"rhs_exchange": {
			"type": "string",
			"description": "Exchange for right-hand side instrument (required if rhs_type=instrument)"
		},
		"rhs_tradingsymbol": {
			"type": "string",
			"description": "Trading symbol for right-hand side instrument (required if rhs_type=instrument)"
		},
		"rhs_attribute": {
			"type": "string",
			"description": "Attribute for right-hand side (required if rhs_type=instrument)"
		},
		"basket": {
			"type": "string",
			"description": "JSON string containing basket configuration. Required when alert_type=ato"
		}
	},
	"required": ["mode", "uuids"]
}`)

func (*AlertsTool) Definition() *mcp.Tool {
	return NewTool("alerts",
		"Manage Kite price alerts - create, modify, delete, and retrieve alerts with optional history",
		alertsSchema,
	)
}

func (*AlertsTool) Handler(manager *kc.Manager) ToolHandler {
	handler := NewToolHandler(manager)
	return func(request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := GetArguments(request)
		mode := SafeAssertString(args["mode"], "")

		switch mode {
		case "get":
			return handler.WithKiteClient(request, "alerts_get", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				return handleGetAlerts(handler, args, client)
			})
		case "create":
			return handler.WithKiteClient(request, "alerts_create", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				return handleCreateAlert(handler, args, client)
			})
		case "modify":
			return handler.WithKiteClient(request, "alerts_modify", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				return handleModifyAlert(handler, args, client)
			})
		case "delete":
			return handler.WithKiteClient(request, "alerts_delete", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				return handleDeleteAlerts(handler, args, client)
			})
		default:
			return NewToolResultError("Invalid mode. Must be one of: get, create, modify, delete"), nil
		}
	}
}

func handleGetAlerts(handler *BaseToolHandler, args map[string]interface{}, client *kiteconnect.Client) (*mcp.CallToolResult, error) {
	uuids := SafeAssertStringArray(args["uuids"])
	includeHistory := SafeAssertBool(args["history"], false)
	status := SafeAssertString(args["status"], "")
	alertType := SafeAssertString(args["type"], "")

	// Build filters
	filters := make(map[string]string)
	if status != "" {
		filters["status"] = status
	}
	if alertType != "" {
		filters["type"] = alertType
	}

	// Handle single UUID
	if len(uuids) == 1 {
		uuid := uuids[0]
		alert, err := client.GetAlert(uuid)
		if err != nil {
			return NewToolResultError(fmt.Sprintf("Failed to get alert %s: %v", uuid, err)), nil
		}

		response := map[string]interface{}{
			"alert": alert,
		}

		if includeHistory {
			history, err := client.GetAlertHistory(uuid)
			if err != nil {
				handler.manager.Logger.Warn("Failed to get alert history", "uuid", uuid, "error", err)
			} else {
				response["history"] = history
			}
		}

		return handler.MarshalResponse(response, "alerts_get")
	}

	// Handle multiple UUIDs or no UUIDs
	var alerts []kiteconnect.Alert
	var err error

	if len(uuids) > 0 {
		// Get all alerts and filter by UUIDs
		allAlerts, err := client.GetAlerts(nil)
		if err != nil {
			return NewToolResultError(fmt.Sprintf("Failed to get alerts: %v", err)), nil
		}

		// Filter by provided UUIDs
		uuidSet := make(map[string]bool)
		for _, uuid := range uuids {
			uuidSet[uuid] = true
		}

		for _, alert := range allAlerts {
			if uuidSet[alert.UUID] {
				alerts = append(alerts, alert)
			}
		}
	} else {
		// Get all alerts with filters
		alerts, err = client.GetAlerts(filters)
		if err != nil {
			return NewToolResultError(fmt.Sprintf("Failed to get alerts: %v", err)), nil
		}
	}

	response := map[string]interface{}{
		"alerts": alerts,
	}

	// Add history if requested
	if includeHistory && len(alerts) > 0 {
		histories := make(map[string]interface{})
		for _, alert := range alerts {
			history, err := client.GetAlertHistory(alert.UUID)
			if err != nil {
				handler.manager.Logger.Warn("Failed to get alert history", "uuid", alert.UUID, "error", err)
			} else {
				histories[alert.UUID] = history
			}
		}
		response["histories"] = histories
	}

	return handler.MarshalResponse(response, "alerts_get")
}

func handleCreateAlert(handler *BaseToolHandler, args map[string]interface{}, client *kiteconnect.Client) (*mcp.CallToolResult, error) {
	if err := validateAlertParams(args, true); err != nil {
		return NewToolResultError(err.Error()), nil
	}

	alertParams, err := buildAlertParams(args)
	if err != nil {
		return NewToolResultError(fmt.Sprintf("Failed to build alert parameters: %v", err)), nil
	}

	alert, err := client.CreateAlert(alertParams)
	if err != nil {
		return NewToolResultError(fmt.Sprintf("Failed to create alert: %v", err)), nil
	}

	return handler.MarshalResponse(map[string]interface{}{
		"alert": alert,
	}, "alerts_create")
}

func handleModifyAlert(handler *BaseToolHandler, args map[string]interface{}, client *kiteconnect.Client) (*mcp.CallToolResult, error) {
	uuids := SafeAssertStringArray(args["uuids"])
	if len(uuids) == 0 {
		return NewToolResultError("UUID is required for modify mode"), nil
	}

	if err := validateAlertParams(args, false); err != nil {
		return NewToolResultError(err.Error()), nil
	}

	alertParams, err := buildAlertParams(args)
	if err != nil {
		return NewToolResultError(fmt.Sprintf("Failed to build alert parameters: %v", err)), nil
	}

	alert, err := client.ModifyAlert(uuids[0], alertParams)
	if err != nil {
		return NewToolResultError(fmt.Sprintf("Failed to modify alert %s: %v", uuids[0], err)), nil
	}

	return handler.MarshalResponse(map[string]interface{}{
		"alert": alert,
	}, "alerts_modify")
}

func handleDeleteAlerts(handler *BaseToolHandler, args map[string]interface{}, client *kiteconnect.Client) (*mcp.CallToolResult, error) {
	uuids := SafeAssertStringArray(args["uuids"])
	if len(uuids) == 0 {
		return NewToolResultError("At least one UUID is required for delete mode"), nil
	}

	err := client.DeleteAlerts(uuids...)
	if err != nil {
		return NewToolResultError(fmt.Sprintf("Failed to delete alerts: %v", err)), nil
	}

	return handler.MarshalResponse(map[string]interface{}{
		"success":       true,
		"deleted_uuids": uuids,
		"deleted_count": len(uuids),
	}, "alerts_delete")
}

func validateAlertParams(args map[string]interface{}, isCreate bool) error {
	// Validate required parameters
	requiredParams := []string{"name", "alert_type", "lhs_exchange", "lhs_tradingsymbol", "lhs_attribute", "operator", "rhs_type"}
	for _, param := range requiredParams {
		if SafeAssertString(args[param], "") == "" {
			return ValidationError{Parameter: param, Message: "is required"}
		}
	}

	// Validate RHS parameters based on type
	rhsType := SafeAssertString(args["rhs_type"], "")
	switch rhsType {
	case "constant":
		if _, ok := args["rhs_constant"]; !ok {
			return ValidationError{Parameter: "rhs_constant", Message: "is required when rhs_type=constant"}
		}
	case "instrument":
		requiredInstrumentParams := []string{"rhs_exchange", "rhs_tradingsymbol", "rhs_attribute"}
		for _, param := range requiredInstrumentParams {
			if SafeAssertString(args[param], "") == "" {
				return ValidationError{Parameter: param, Message: "is required when rhs_type=instrument"}
			}
		}
	default:
		return ValidationError{Parameter: "rhs_type", Message: "must be 'constant' or 'instrument'"}
	}

	// Validate basket for ATO alerts
	alertType := SafeAssertString(args["alert_type"], "")
	if alertType == "ato" {
		basketStr := SafeAssertString(args["basket"], "")
		if basketStr == "" {
			return ValidationError{Parameter: "basket", Message: "is required when alert_type=ato"}
		}

		var basket kiteconnect.Basket
		if err := json.Unmarshal([]byte(basketStr), &basket); err != nil {
			return ValidationError{Parameter: "basket", Message: "must be valid JSON"}
		}
		if len(basket.Items) == 0 {
			return ValidationError{Parameter: "basket", Message: "must contain at least one basket item when alert_type=ato"}
		}
	}

	return nil
}

func buildAlertParams(args map[string]interface{}) (kiteconnect.AlertParams, error) {
	// Validation should have already guaranteed basket presence for ATO alerts.
	params := kiteconnect.AlertParams{
		Name:             SafeAssertString(args["name"], ""),
		Type:             kiteconnect.AlertType(SafeAssertString(args["alert_type"], "")),
		LHSExchange:      SafeAssertString(args["lhs_exchange"], ""),
		LHSTradingSymbol: SafeAssertString(args["lhs_tradingsymbol"], ""),
		LHSAttribute:     SafeAssertString(args["lhs_attribute"], ""),
		Operator:         kiteconnect.AlertOperator(SafeAssertString(args["operator"], "")),
		RHSType:          SafeAssertString(args["rhs_type"], ""),
		RHSConstant:      SafeAssertFloat64(args["rhs_constant"], 0),
		RHSExchange:      SafeAssertString(args["rhs_exchange"], ""),
		RHSTradingSymbol: SafeAssertString(args["rhs_tradingsymbol"], ""),
		RHSAttribute:     SafeAssertString(args["rhs_attribute"], ""),
	}

	// Parse basket if provided
	if basketStr, ok := args["basket"]; ok && SafeAssertString(basketStr, "") != "" {
		var basket kiteconnect.Basket
		if err := json.Unmarshal([]byte(SafeAssertString(basketStr, "")), &basket); err != nil {
			return params, fmt.Errorf("failed to parse basket JSON: %v", err)
		}
		params.Basket = &basket
	}

	return params, nil
}
