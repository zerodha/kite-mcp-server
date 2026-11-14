package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

const (
	alertRHSTypeConstant   = "constant"
	alertRHSTypeInstrument = "instrument"
	alertDefaultAttribute  = "LastTradedPrice"
)

// alertConditionOptions returns the tool options shared by create_alert and modify_alert.
func alertConditionOptions() []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithString("name",
			mcp.Description("Name of the alert"),
			mcp.Required(),
		),
		mcp.WithString("type",
			mcp.Description("Alert type. 'simple' only notifies when triggered, 'ato' (Alert Triggers Order) places the orders in 'basket' when triggered"),
			mcp.Required(),
			mcp.DefaultString("simple"),
			mcp.Enum("simple", "ato"),
		),
		mcp.WithString("lhs_exchange",
			mcp.Description("Exchange of the instrument to monitor (use INDICES for indices like NIFTY 50)"),
			mcp.Required(),
			mcp.DefaultString("NSE"),
			mcp.Enum("NSE", "BSE", "NFO", "CDS", "BCD", "MCX", "INDICES"),
		),
		mcp.WithString("lhs_tradingsymbol",
			mcp.Description("Trading symbol of the instrument to monitor"),
			mcp.Required(),
		),
		mcp.WithString("lhs_attribute",
			mcp.Description("Attribute of the instrument to monitor. Default: LastTradedPrice"),
			mcp.DefaultString(alertDefaultAttribute),
		),
		mcp.WithString("operator",
			mcp.Description("Comparison operator between the left-hand side and the right-hand side"),
			mcp.Required(),
			mcp.Enum("<=", ">=", "<", ">", "=="),
		),
		mcp.WithString("rhs_type",
			mcp.Description("Type of the right-hand side. 'constant' compares against rhs_constant, 'instrument' compares against another instrument"),
			mcp.Required(),
			mcp.DefaultString(alertRHSTypeConstant),
			mcp.Enum(alertRHSTypeConstant, alertRHSTypeInstrument),
		),
		mcp.WithNumber("rhs_constant",
			mcp.Description("Value to compare against (required when rhs_type is 'constant')"),
		),
		mcp.WithString("rhs_exchange",
			mcp.Description("Exchange of the instrument to compare against (required when rhs_type is 'instrument')"),
			mcp.Enum("NSE", "BSE", "NFO", "CDS", "BCD", "MCX", "INDICES"),
		),
		mcp.WithString("rhs_tradingsymbol",
			mcp.Description("Trading symbol of the instrument to compare against (required when rhs_type is 'instrument')"),
		),
		mcp.WithString("rhs_attribute",
			mcp.Description("Attribute of the instrument to compare against (when rhs_type is 'instrument'). Default: LastTradedPrice"),
		),
		mcp.WithString("basket",
			mcp.Description(`Basket of orders to place when the alert triggers, as a JSON string (required when type is 'ato'). Format: {"name": "...", "type": "alert", "tags": [], "items": [{"type": "insert", "tradingsymbol": "INFY", "exchange": "NSE", "weight": 0, "params": {"transaction_type": "BUY", "product": "CNC", "order_type": "LIMIT", "validity": "DAY", "quantity": 1, "price": 1500, "variety": "regular"}}]}`),
		),
	}
}

// buildAlertParams validates the alert arguments and maps them into kiteconnect.AlertParams.
func buildAlertParams(args map[string]interface{}) (kiteconnect.AlertParams, error) {
	var params kiteconnect.AlertParams

	if err := ValidateRequired(args, "name", "type", "lhs_exchange", "lhs_tradingsymbol", "operator", "rhs_type"); err != nil {
		return params, err
	}

	params = kiteconnect.AlertParams{
		Name:             SafeAssertString(args["name"], ""),
		Type:             kiteconnect.AlertType(SafeAssertString(args["type"], string(kiteconnect.AlertTypeSimple))),
		LHSExchange:      SafeAssertString(args["lhs_exchange"], "NSE"),
		LHSTradingSymbol: SafeAssertString(args["lhs_tradingsymbol"], ""),
		LHSAttribute:     SafeAssertString(args["lhs_attribute"], alertDefaultAttribute),
		Operator:         kiteconnect.AlertOperator(SafeAssertString(args["operator"], "")),
		RHSType:          SafeAssertString(args["rhs_type"], alertRHSTypeConstant),
	}
	if params.LHSAttribute == "" {
		params.LHSAttribute = alertDefaultAttribute
	}

	switch params.Operator {
	case kiteconnect.AlertOperatorLE, kiteconnect.AlertOperatorGE, kiteconnect.AlertOperatorLT, kiteconnect.AlertOperatorGT, kiteconnect.AlertOperatorEQ:
	default:
		return params, ValidationError{Parameter: "operator", Message: "must be one of '<=', '>=', '<', '>', '=='"}
	}

	switch params.RHSType {
	case alertRHSTypeConstant:
		if err := ValidateRequired(args, "rhs_constant"); err != nil {
			return params, err
		}
		// Reject non-numeric values instead of silently comparing against 0.
		switch args["rhs_constant"].(type) {
		case float64, int:
		default:
			return params, ValidationError{Parameter: "rhs_constant", Message: "must be a number"}
		}
		params.RHSConstant = SafeAssertFloat64(args["rhs_constant"], 0.0)
	case alertRHSTypeInstrument:
		if err := ValidateRequired(args, "rhs_exchange", "rhs_tradingsymbol"); err != nil {
			return params, err
		}
		params.RHSExchange = SafeAssertString(args["rhs_exchange"], "")
		params.RHSTradingSymbol = SafeAssertString(args["rhs_tradingsymbol"], "")
		params.RHSAttribute = SafeAssertString(args["rhs_attribute"], alertDefaultAttribute)
		if params.RHSAttribute == "" {
			params.RHSAttribute = alertDefaultAttribute
		}
	default:
		return params, ValidationError{Parameter: "rhs_type", Message: "must be 'constant' or 'instrument'"}
	}

	switch params.Type {
	case kiteconnect.AlertTypeSimple:
	case kiteconnect.AlertTypeATO:
		if err := ValidateRequired(args, "basket"); err != nil {
			return params, err
		}
		basket, err := parseAlertBasket(args["basket"])
		if err != nil {
			return params, err
		}
		params.Basket = basket
	default:
		return params, ValidationError{Parameter: "type", Message: "must be 'simple' or 'ato'"}
	}

	return params, nil
}

// parseAlertBasket parses the basket argument (a JSON string) into a kiteconnect.Basket.
func parseAlertBasket(v interface{}) (*kiteconnect.Basket, error) {
	var basket kiteconnect.Basket
	if err := json.Unmarshal([]byte(SafeAssertString(v, "")), &basket); err != nil {
		return nil, ValidationError{Parameter: "basket", Message: fmt.Sprintf("must be a valid JSON object: %v", err)}
	}

	if len(basket.Items) == 0 {
		return nil, ValidationError{Parameter: "basket", Message: "must contain at least one item"}
	}

	return &basket, nil
}

type GetAlertsTool struct{}

func (*GetAlertsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_alerts",
		mcp.WithDescription("Get all price alerts for the current user, optionally filtered by status. Supports pagination for large datasets."),
		mcp.WithString("status",
			mcp.Description("Filter alerts by status. If not specified, returns alerts of all statuses."),
			mcp.Enum(string(kiteconnect.AlertStatusEnabled), string(kiteconnect.AlertStatusDisabled), string(kiteconnect.AlertStatusDeleted)),
		),
		mcp.WithNumber("from",
			mcp.Description("Starting index for pagination (0-based). Default: 0"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of alerts to return. If not specified, returns all alerts. When specified, response includes pagination metadata."),
		),
	)
}

func (*GetAlertsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "get_alerts")
		args := request.GetArguments()

		filters := map[string]string{}
		if status := SafeAssertString(args["status"], ""); status != "" {
			filters["status"] = status
		}

		return handler.WithSession(ctx, "get_alerts", func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
			alerts, err := session.Kite.Client.GetAlerts(filters)
			if err != nil {
				handler.manager.Logger.Error("Failed to get alerts", "error", err)
				handler.trackToolError(ctx, "get_alerts", "api_error")
				return mcp.NewToolResultError("Failed to get alerts"), nil
			}

			// Apply pagination if limit is specified
			params := ParsePaginationParams(args)
			paginatedData := ApplyPagination(alerts, params)

			var responseData interface{}
			if params.Limit > 0 {
				responseData = CreatePaginatedResponse(alerts, paginatedData, params, len(alerts))
			} else {
				responseData = paginatedData
			}

			return handler.MarshalResponse(responseData, "get_alerts")
		})
	}
}

type GetAlertTool struct{}

func (*GetAlertTool) Tool() mcp.Tool {
	return mcp.NewTool("get_alert",
		mcp.WithDescription("Get a specific price alert by its UUID"),
		mcp.WithString("uuid",
			mcp.Description("UUID of the alert to fetch"),
			mcp.Required(),
		),
	)
}

func (*GetAlertTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "get_alert")
		args := request.GetArguments()

		// Validate required parameters
		if err := ValidateRequired(args, "uuid"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uuid := SafeAssertString(args["uuid"], "")

		return handler.WithSession(ctx, "get_alert", func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
			alert, err := session.Kite.Client.GetAlert(uuid)
			if err != nil {
				handler.manager.Logger.Error("Failed to get alert", "error", err)
				return mcp.NewToolResultError("Failed to get alert"), nil
			}

			return handler.MarshalResponse(alert, "get_alert")
		})
	}
}

type GetAlertHistoryTool struct{}

func (*GetAlertHistoryTool) Tool() mcp.Tool {
	return mcp.NewTool("get_alert_history",
		mcp.WithDescription("Get the trigger history of a specific price alert, including market data at the time it triggered"),
		mcp.WithString("uuid",
			mcp.Description("UUID of the alert to fetch history for"),
			mcp.Required(),
		),
	)
}

func (*GetAlertHistoryTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "get_alert_history")
		args := request.GetArguments()

		// Validate required parameters
		if err := ValidateRequired(args, "uuid"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uuid := SafeAssertString(args["uuid"], "")

		return handler.WithSession(ctx, "get_alert_history", func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
			history, err := session.Kite.Client.GetAlertHistory(uuid)
			if err != nil {
				handler.manager.Logger.Error("Failed to get alert history", "error", err)
				return mcp.NewToolResultError("Failed to get alert history"), nil
			}

			return handler.MarshalResponse(history, "get_alert_history")
		})
	}
}

type CreateAlertTool struct{}

func (*CreateAlertTool) Tool() mcp.Tool {
	opts := append([]mcp.ToolOption{
		mcp.WithDescription("Create a price alert. A 'simple' alert notifies when the condition is met, an 'ato' (Alert Triggers Order) alert also places the orders in its basket."),
	}, alertConditionOptions()...)
	return mcp.NewTool("create_alert", opts...)
}

func (*CreateAlertTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "create_alert")
		args := request.GetArguments()

		// Validate and map parameters
		alertParams, err := buildAlertParams(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return handler.WithSession(ctx, "create_alert", func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
			resp, err := session.Kite.Client.CreateAlert(alertParams)
			if err != nil {
				handler.manager.Logger.Error("Failed to create alert", "error", err)
				return mcp.NewToolResultError("Failed to create alert"), nil
			}

			return handler.MarshalResponse(resp, "create_alert")
		})
	}
}

type ModifyAlertTool struct{}

func (*ModifyAlertTool) Tool() mcp.Tool {
	opts := append([]mcp.ToolOption{
		mcp.WithDescription("Modify an existing price alert. All alert parameters must be provided as they replace the existing ones; use get_alert to fetch the current values first."),
		mcp.WithString("uuid",
			mcp.Description("UUID of the alert to modify"),
			mcp.Required(),
		),
	}, alertConditionOptions()...)
	return mcp.NewTool("modify_alert", opts...)
}

func (*ModifyAlertTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "modify_alert")
		args := request.GetArguments()

		// Validate required parameters
		if err := ValidateRequired(args, "uuid"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uuid := SafeAssertString(args["uuid"], "")

		alertParams, err := buildAlertParams(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return handler.WithSession(ctx, "modify_alert", func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
			resp, err := session.Kite.Client.ModifyAlert(uuid, alertParams)
			if err != nil {
				handler.manager.Logger.Error("Failed to modify alert", "error", err)
				return mcp.NewToolResultError("Failed to modify alert"), nil
			}

			return handler.MarshalResponse(resp, "modify_alert")
		})
	}
}

type DeleteAlertsTool struct{}

func (*DeleteAlertsTool) Tool() mcp.Tool {
	return mcp.NewTool("delete_alerts",
		mcp.WithDescription("Delete one or more price alerts by UUID"),
		mcp.WithArray("uuids",
			mcp.Description("UUIDs of the alerts to delete. Eg. ['550e8400-e29b-41d4-a716-446655440000']"),
			mcp.Required(),
			mcp.Items(map[string]any{
				"type": "string",
			}),
		),
	)
}

func (*DeleteAlertsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "delete_alerts")
		args := request.GetArguments()

		// Validate required parameters
		if err := ValidateRequired(args, "uuids"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uuids := SafeAssertStringArray(args["uuids"])
		if len(uuids) == 0 {
			return mcp.NewToolResultError(ValidationError{Parameter: "uuids", Message: "must contain at least one UUID"}.Error()), nil
		}

		return handler.WithSession(ctx, "delete_alerts", func(session *kc.KiteSessionData) (*mcp.CallToolResult, error) {
			if err := session.Kite.Client.DeleteAlerts(uuids...); err != nil {
				handler.manager.Logger.Error("Failed to delete alerts", "error", err)
				return mcp.NewToolResultError("Failed to delete alerts"), nil
			}

			return handler.MarshalResponse(map[string][]string{"deleted": uuids}, "delete_alerts")
		})
	}
}
