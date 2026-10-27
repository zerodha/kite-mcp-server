package mcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

type GTTTool struct{}

var gttSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"mode": {
			"type": "string",
			"description": "Operation mode: list=all GTT orders, place=create new GTT, modify=update existing GTT, delete=remove a GTT",
			"enum": ["list", "place", "modify", "delete"]
		},
		"trigger_id": {
			"type": "number",
			"description": "GTT trigger ID (required for modify and delete modes)"
		},
		"exchange": {
			"type": "string",
			"description": "Exchange (required for place and modify modes)",
			"default": "NSE",
			"enum": ["NSE", "BSE", "MCX", "NFO", "BFO"]
		},
		"tradingsymbol": {
			"type": "string",
			"description": "Trading symbol (required for place and modify modes)"
		},
		"last_price": {
			"type": "number",
			"description": "Current price of the instrument (required for place and modify modes)"
		},
		"transaction_type": {
			"type": "string",
			"description": "BUY or SELL (required for place and modify modes)",
			"enum": ["BUY", "SELL"]
		},
		"product": {
			"type": "string",
			"description": "Product type (required for place mode)",
			"enum": ["CNC", "NRML", "MIS", "MTF"]
		},
		"trigger_type": {
			"type": "string",
			"description": "GTT trigger type (required for place and modify modes): single=one trigger, two-leg=OCO (one-cancels-other) with upper and lower triggers",
			"enum": ["single", "two-leg"]
		},
		"trigger_value": {
			"type": "number",
			"description": "Trigger price (for single-leg GTT)"
		},
		"quantity": {
			"type": "number",
			"description": "Order quantity (for single-leg GTT)"
		},
		"limit_price": {
			"type": "number",
			"description": "Limit price for the resulting order (for single-leg GTT)"
		},
		"upper_trigger_value": {
			"type": "number",
			"description": "Upper trigger price (for two-leg GTT)"
		},
		"upper_quantity": {
			"type": "number",
			"description": "Quantity for the upper leg (for two-leg GTT)"
		},
		"upper_limit_price": {
			"type": "number",
			"description": "Limit price for the upper leg (for two-leg GTT)"
		},
		"lower_trigger_value": {
			"type": "number",
			"description": "Lower trigger price (for two-leg GTT)"
		},
		"lower_quantity": {
			"type": "number",
			"description": "Quantity for the lower leg (for two-leg GTT)"
		},
		"lower_limit_price": {
			"type": "number",
			"description": "Limit price for the lower leg (for two-leg GTT)"
		},
		"from": {
			"type": "integer",
			"minimum": 0,
			"maximum": 2147483647,
			"default": 0,
			"description": "Starting index for pagination (0-based). Applies to list mode. Default: 0"
		},
		"limit": {
			"type": "integer",
			"minimum": 1,
			"maximum": 500,
			"default": 100,
			"description": "Page size (integer 1-500, default 100). Responses always include pagination metadata"
		}
	},
	"required": ["mode"]
}`)

func (*GTTTool) Definition() *mcp.Tool {
	return NewTool("gtt",
		"List mode returns data/pagination with default limit 100 and maximum 500. Manage GTT (Good Till Triggered) orders that persist across trading sessions and execute when a price condition is met. Use mode=list to see all GTTs, mode=place to create a new GTT (single-leg or two-leg OCO), mode=modify to update an existing GTT, mode=delete to remove a GTT. To show a read-only interactive GTT monitor with trigger distances, use open_kite_view when available with view=gtt. Continue to use gtt directly for mutations.",
		gttSchema,
	)
}

func (*GTTTool) Handler(manager *kc.Manager) ToolHandler {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := GetArguments(request)
		mode := SafeAssertString(args["mode"], "")

		switch mode {
		case "list":
			return PaginatedToolHandler(manager, "gtt_list", func(client *kiteconnect.Client) ([]interface{}, error) {
				gttBook, err := client.GetGTTs()
				if err != nil {
					return nil, err
				}
				result := make([]interface{}, len(gttBook))
				for i, gtt := range gttBook {
					result[i] = gtt
				}
				return result, nil
			})(ctx, request)

		case "place":
			if err := ValidateRequired(args, "exchange", "tradingsymbol", "last_price", "transaction_type", "product", "trigger_type"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			gttParams, err := buildGTTParams(args)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return handler.WithKiteClient(request, "gtt_place", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				resp, err := client.PlaceGTT(gttParams)
				if err != nil {
					handler.manager.Logger.Error("Failed to place GTT order", "error", err)
					return NewToolResultError("Failed to place GTT order"), nil
				}
				return handler.MarshalResponse(resp, "gtt_place")
			})

		case "modify":
			if err := ValidateRequired(args, "trigger_id", "exchange", "tradingsymbol", "last_price", "transaction_type", "trigger_type"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			triggerID := SafeAssertInt(args["trigger_id"], 0)
			gttParams, err := buildGTTParams(args)
			if err != nil {
				return NewToolResultError(err.Error()), nil
			}
			return handler.WithKiteClient(request, "gtt_modify", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				resp, err := client.ModifyGTT(triggerID, gttParams)
				if err != nil {
					handler.manager.Logger.Error("Failed to modify GTT order", "error", err)
					return NewToolResultError("Failed to modify GTT order"), nil
				}
				return handler.MarshalResponse(resp, "gtt_modify")
			})

		case "delete":
			if err := ValidateRequired(args, "trigger_id"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			triggerID := SafeAssertInt(args["trigger_id"], 0)
			return handler.WithKiteClient(request, "gtt_delete", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				resp, err := client.DeleteGTT(triggerID)
				if err != nil {
					handler.manager.Logger.Error("Failed to delete GTT order", "error", err)
					return NewToolResultError("Failed to delete GTT order"), nil
				}
				return handler.MarshalResponse(resp, "gtt_delete")
			})

		default:
			return NewToolResultError("Invalid mode. Must be one of: list, place, modify, delete"), nil
		}
	}
}

func buildGTTParams(args map[string]interface{}) (kiteconnect.GTTParams, error) {
	gttParams := kiteconnect.GTTParams{
		Exchange:        SafeAssertString(args["exchange"], "NSE"),
		Tradingsymbol:   SafeAssertString(args["tradingsymbol"], ""),
		LastPrice:       SafeAssertFloat64(args["last_price"], 0.0),
		TransactionType: SafeAssertString(args["transaction_type"], ""),
		Product:         SafeAssertString(args["product"], ""),
	}

	triggerType := SafeAssertString(args["trigger_type"], "")
	switch triggerType {
	case "single":
		gttParams.Trigger = &kiteconnect.GTTSingleLegTrigger{
			TriggerParams: kiteconnect.TriggerParams{
				TriggerValue: SafeAssertFloat64(args["trigger_value"], 0.0),
				Quantity:     SafeAssertFloat64(args["quantity"], 0.0),
				LimitPrice:   SafeAssertFloat64(args["limit_price"], 0.0),
			},
		}
	case "two-leg":
		gttParams.Trigger = &kiteconnect.GTTOneCancelsOtherTrigger{
			Upper: kiteconnect.TriggerParams{
				TriggerValue: SafeAssertFloat64(args["upper_trigger_value"], 0.0),
				Quantity:     SafeAssertFloat64(args["upper_quantity"], 0.0),
				LimitPrice:   SafeAssertFloat64(args["upper_limit_price"], 0.0),
			},
			Lower: kiteconnect.TriggerParams{
				TriggerValue: SafeAssertFloat64(args["lower_trigger_value"], 0.0),
				Quantity:     SafeAssertFloat64(args["lower_quantity"], 0.0),
				LimitPrice:   SafeAssertFloat64(args["lower_limit_price"], 0.0),
			},
		}
	default:
		return gttParams, ValidationError{Parameter: "trigger_type", Message: "must be 'single' or 'two-leg'"}
	}

	return gttParams, nil
}
