package mcp

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

const (
	alertTypeSimple     = "simple"
	alertTypeATO        = "ato"
	alertRHSConstant    = "constant"
	alertRHSInstrument  = "instrument"
	alertBasketRequired = "basket is required for ato alerts"
)

func alertToolFields() []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithString("name",
			mcp.Description("Name of the alert"),
			mcp.Required(),
		),
		mcp.WithString("type",
			mcp.Description("Alert type"),
			mcp.Required(),
			mcp.Enum(alertTypeSimple, alertTypeATO),
		),
		mcp.WithString("lhs_exchange",
			mcp.Description("Exchange for the left-hand side instrument"),
			mcp.Required(),
			mcp.Enum("NSE", "BSE", "NFO", "CDS", "BCD", "MCX", "INDICES"),
		),
		mcp.WithString("lhs_tradingsymbol",
			mcp.Description("Trading symbol for the left-hand side instrument"),
			mcp.Required(),
		),
		mcp.WithString("lhs_attribute",
			mcp.Description("Attribute to monitor, for example LastTradedPrice"),
			mcp.Required(),
		),
		mcp.WithString("operator",
			mcp.Description("Comparison operator"),
			mcp.Required(),
			mcp.Enum("<=", ">=", "<", ">", "=="),
		),
		mcp.WithString("rhs_type",
			mcp.Description("Right-hand side comparison type"),
			mcp.Required(),
			mcp.Enum(alertRHSConstant, alertRHSInstrument),
		),
		mcp.WithNumber("rhs_constant",
			mcp.Description("Constant value to compare against when rhs_type is constant"),
		),
		mcp.WithString("rhs_exchange",
			mcp.Description("Exchange for the right-hand side instrument when rhs_type is instrument"),
			mcp.Enum("NSE", "BSE", "NFO", "CDS", "BCD", "MCX", "INDICES"),
		),
		mcp.WithString("rhs_tradingsymbol",
			mcp.Description("Trading symbol for the right-hand side instrument when rhs_type is instrument"),
		),
		mcp.WithString("rhs_attribute",
			mcp.Description("Attribute for the right-hand side instrument when rhs_type is instrument"),
		),
		mcp.WithString("basket",
			mcp.Description("JSON basket payload required for ato alerts"),
		),
	}
}

func buildAlertParams(args map[string]interface{}) (kiteconnect.AlertParams, error) {
	if err := ValidateRequired(args, "name", "type", "lhs_exchange", "lhs_tradingsymbol", "lhs_attribute", "operator", "rhs_type"); err != nil {
		return kiteconnect.AlertParams{}, err
	}

	params := kiteconnect.AlertParams{
		Name:             SafeAssertString(args["name"], ""),
		Type:             kiteconnect.AlertType(SafeAssertString(args["type"], alertTypeSimple)),
		LHSExchange:      SafeAssertString(args["lhs_exchange"], ""),
		LHSTradingSymbol: SafeAssertString(args["lhs_tradingsymbol"], ""),
		LHSAttribute:     SafeAssertString(args["lhs_attribute"], ""),
		Operator:         kiteconnect.AlertOperator(SafeAssertString(args["operator"], "")),
		RHSType:          SafeAssertString(args["rhs_type"], ""),
	}

	switch params.RHSType {
	case alertRHSConstant:
		if err := ValidateRequired(args, "rhs_constant"); err != nil {
			return kiteconnect.AlertParams{}, err
		}
		params.RHSConstant = SafeAssertFloat64(args["rhs_constant"], 0)
	case alertRHSInstrument:
		if err := ValidateRequired(args, "rhs_exchange", "rhs_tradingsymbol", "rhs_attribute"); err != nil {
			return kiteconnect.AlertParams{}, err
		}
		params.RHSExchange = SafeAssertString(args["rhs_exchange"], "")
		params.RHSTradingSymbol = SafeAssertString(args["rhs_tradingsymbol"], "")
		params.RHSAttribute = SafeAssertString(args["rhs_attribute"], "")
	default:
		return kiteconnect.AlertParams{}, ValidationError{Parameter: "rhs_type", Message: "must be constant or instrument"}
	}

	if params.Type == alertTypeATO {
		basketJSON := SafeAssertString(args["basket"], "")
		if basketJSON == "" {
			return kiteconnect.AlertParams{}, ValidationError{Parameter: "basket", Message: alertBasketRequired}
		}
		var basket kiteconnect.Basket
		if err := json.Unmarshal([]byte(basketJSON), &basket); err != nil {
			return kiteconnect.AlertParams{}, ValidationError{Parameter: "basket", Message: "must be valid JSON"}
		}
		params.Basket = &basket
	}

	return params, nil
}

type GetAlertsTool struct{}

func (*GetAlertsTool) Tool() mcp.Tool {
	return mcp.NewTool("get_alerts",
		mcp.WithDescription("Get Kite alerts, optionally filtered by status and paginated by Kite"),
		mcp.WithString("status",
			mcp.Description("Filter alerts by status"),
			mcp.Enum("enabled", "disabled", "deleted"),
		),
		mcp.WithNumber("page",
			mcp.Description("Kite alerts page number"),
		),
		mcp.WithNumber("page_size",
			mcp.Description("Number of alerts per page"),
		),
	)
}

func (*GetAlertsTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "get_alerts")
		args := request.GetArguments()
		filters := map[string]string{}
		for _, key := range []string{"status", "page", "page_size"} {
			if value := SafeAssertString(args[key], ""); value != "" {
				filters[key] = value
			}
		}

		return handler.HandleAPICall(ctx, "get_alerts", func(session *kc.KiteSessionData) (interface{}, error) {
			return session.Kite.Client.GetAlerts(filters)
		})
	}
}

type GetAlertTool struct{}

func (*GetAlertTool) Tool() mcp.Tool {
	return mcp.NewTool("get_alert",
		mcp.WithDescription("Get a Kite alert by UUID"),
		mcp.WithString("uuid",
			mcp.Description("Alert UUID"),
			mcp.Required(),
		),
	)
}

func (*GetAlertTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "get_alert")
		args := request.GetArguments()
		if err := ValidateRequired(args, "uuid"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uuid := SafeAssertString(args["uuid"], "")
		return handler.HandleAPICall(ctx, "get_alert", func(session *kc.KiteSessionData) (interface{}, error) {
			return session.Kite.Client.GetAlert(uuid)
		})
	}
}

type GetAlertHistoryTool struct{}

func (*GetAlertHistoryTool) Tool() mcp.Tool {
	return mcp.NewTool("get_alert_history",
		mcp.WithDescription("Get trigger history for a Kite alert by UUID"),
		mcp.WithString("uuid",
			mcp.Description("Alert UUID"),
			mcp.Required(),
		),
	)
}

func (*GetAlertHistoryTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "get_alert_history")
		args := request.GetArguments()
		if err := ValidateRequired(args, "uuid"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uuid := SafeAssertString(args["uuid"], "")
		return handler.HandleAPICall(ctx, "get_alert_history", func(session *kc.KiteSessionData) (interface{}, error) {
			return session.Kite.Client.GetAlertHistory(uuid)
		})
	}
}

type CreateAlertTool struct{}

func (*CreateAlertTool) Tool() mcp.Tool {
	options := append([]mcp.ToolOption{mcp.WithDescription("Create a Kite alert")}, alertToolFields()...)
	return mcp.NewTool("create_alert", options...)
}

func (*CreateAlertTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "create_alert")
		params, err := buildAlertParams(request.GetArguments())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return handler.HandleAPICall(ctx, "create_alert", func(session *kc.KiteSessionData) (interface{}, error) {
			return session.Kite.Client.CreateAlert(params)
		})
	}
}

type ModifyAlertTool struct{}

func (*ModifyAlertTool) Tool() mcp.Tool {
	options := append([]mcp.ToolOption{
		mcp.WithDescription("Modify an existing Kite alert"),
		mcp.WithString("uuid",
			mcp.Description("Alert UUID"),
			mcp.Required(),
		),
	}, alertToolFields()...)
	return mcp.NewTool("modify_alert", options...)
}

func (*ModifyAlertTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handler.trackToolCall(ctx, "modify_alert")
		args := request.GetArguments()
		if err := ValidateRequired(args, "uuid"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uuid := SafeAssertString(args["uuid"], "")
		params, err := buildAlertParams(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return handler.HandleAPICall(ctx, "modify_alert", func(session *kc.KiteSessionData) (interface{}, error) {
			return session.Kite.Client.ModifyAlert(uuid, params)
		})
	}
}

type DeleteAlertsTool struct{}

func (*DeleteAlertsTool) Tool() mcp.Tool {
	return mcp.NewTool("delete_alerts",
		mcp.WithDescription("Delete one or more Kite alerts by UUID"),
		mcp.WithArray("uuids",
			mcp.Description("Alert UUIDs to delete"),
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
		if err := ValidateRequired(args, "uuids"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uuids := SafeAssertStringArray(args["uuids"])
		if len(uuids) == 0 {
			return mcp.NewToolResultError("At least one alert UUID must be specified"), nil
		}

		return handler.HandleAPICall(ctx, "delete_alerts", func(session *kc.KiteSessionData) (interface{}, error) {
			if err := session.Kite.Client.DeleteAlerts(uuids...); err != nil {
				return nil, err
			}
			return map[string]any{"deleted": uuids}, nil
		})
	}
}
