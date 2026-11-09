package mcp

import (
	"context"
	"encoding/json"
	"math"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

type OrdersTool struct{}

var ordersSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"mode": {
			"type": "string",
			"description": "Operation mode: list=all orders today, history=status trail for an order, trades=fills for an order, all_trades=all trades today, place=place new order, modify=modify open order, cancel=cancel open order, approval_status=check a browser approval outcome, permissions=manage future order permission",
			"enum": ["list", "history", "trades", "all_trades", "place", "modify", "cancel", "approval_status", "permissions"]
		},
		"approval_method": {
			"type": "string",
			"enum": ["auto", "browser"],
			"description": "Select browser automatically for ChatGPT mobile or clients known not to display approval forms. auto uses client capabilities. Do not ask the user to request a link. This never grants approval."
		},
		"approval_id": {
			"type": "string",
			"maxLength": 64,
			"description": "Browser approval ID (required for approval_status). Check this rather than repeating a pending order request."
		},
		"order_id": {
			"type": "string",
			"description": "Order ID (required for history, trades, modify, cancel modes)"
		},
		"variety": {
			"type": "string",
			"description": "Order variety (required for place, modify, cancel modes)",
			"default": "regular",
			"enum": ["regular", "co", "amo", "iceberg", "auction"]
		},
		"exchange": {
			"type": "string",
			"description": "Exchange (required for place mode)",
			"default": "NSE",
			"enum": ["NSE", "BSE", "MCX", "NFO", "BFO"]
		},
		"tradingsymbol": {
			"type": "string",
			"description": "Trading symbol (required for place mode)"
		},
		"transaction_type": {
			"type": "string",
			"description": "BUY or SELL (required for place mode)",
			"enum": ["BUY", "SELL"]
		},
		"quantity": {
			"type": "integer",
			"description": "Quantity in units, not lots (required for place mode); multiply lots by the instrument lot size",
			"default": 1,
			"minimum": 1,
			"maximum": 2147483647
		},
		"product": {
			"type": "string",
			"description": "Product type (required for place mode): CNC=delivery, MIS=intraday, NRML=F&O overnight, MTF=margin funding",
			"enum": ["CNC", "NRML", "MIS", "MTF"]
		},
		"order_type": {
			"type": "string",
			"description": "Order type (required for place and modify modes): MARKET, LIMIT, SL (stop-loss limit), SL-M (stop-loss market)",
			"enum": ["MARKET", "LIMIT", "SL", "SL-M"]
		},
		"price": {
			"type": "number",
			"description": "Limit price for LIMIT/SL placement; supply it when changing the price in modify mode"
		},
		"trigger_price": {
			"type": "number",
			"description": "Trigger price for SL/SL-M placement; supply it when changing the trigger in modify mode"
		},
		"validity": {
			"type": "string",
			"description": "Order validity: DAY, IOC (immediate or cancel), TTL (time-to-live in minutes)",
			"enum": ["DAY", "IOC", "TTL"]
		},
		"validity_ttl": {
			"type": "integer",
			"description": "Order life span in minutes (required when validity=TTL)",
			"minimum": 1,
			"maximum": 2147483647
		},
		"disclosed_quantity": {
			"type": "integer",
			"description": "Quantity disclosed to the market",
			"minimum": 0,
			"maximum": 2147483647
		},
		"iceberg_legs": {
			"type": "integer",
			"description": "Number of legs for iceberg orders",
			"minimum": 1,
			"maximum": 2147483647
		},
		"iceberg_quantity": {
			"type": "integer",
			"description": "Quantity per leg for iceberg orders",
			"minimum": 1,
			"maximum": 2147483647
		},
		"tag": {
			"type": "string",
			"description": "Optional alphanumeric label (max 20 characters)",
			"maxLength": 20
		},
		"market_protection": {
			"type": "number",
			"description": "Market protection percentage for MARKET and SL-M orders. 0=disabled, 1-100=custom %, -1=auto"
		},
		"autoslice": {
			"type": "boolean",
			"description": "Auto-split large orders exceeding freeze quantity limits into smaller child orders (max 50 slices)"
		},
		"from": {
			"type": "integer",
			"minimum": 0,
			"maximum": 2147483647,
			"default": 0,
			"description": "Starting index for pagination (0-based). Applies to list and all_trades modes. Default: 0"
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

func (*OrdersTool) Definition() *mcp.Tool {
	return newDataTool("orders",
		"Manage orders. Placement, modification, and cancellation require user approval. Clients with form elicitation show a form. Other clients receive a browser approval link automatically. The user signs in to the matching Kite account and confirms the order in any browser; no original-browser requirement. For ChatGPT mobile or a client known not to display forms, set approval_method=browser on the initial call. If future_orders_allowed=true, use auto unless the user explicitly requests another review. Do not ask the user to request a link; share the returned approval_url immediately and do not open or approve it on their behalf. Native failures reported to the server fall back to browser review when possible; explicit decline, cancellation, and negative confirmation stop the request. An approval_required result means no order was submitted: do not repeat the order call. Use approval_status with its approval_id to check the outcome. A status of approved_for_later means approval was saved for this exact order once, but nothing was placed. Wait for a new user instruction before making the matching order request with approval_method=auto. Changed order details require new approval unless future_orders_allowed=true. Saving an exact-order approval does not revoke existing permission for any future order. Users can explicitly allow future orders for this client from the browser approval page. Use permissions to show or manage this permission. Use mode=list to see all orders today, mode=history for an order's status trail, mode=trades for fills on a specific order, mode=all_trades for all trades today, mode=place to place a new order, mode=modify to change an open order, mode=cancel to cancel an open order. List and all_trades modes return data/pagination with default limit 100 and maximum 500. To show a read-only interactive order book with timeline and fills, use open_kite_view when available with view=orders. Only request mutations when the user asks for the action. Approval observations include approval_method_used, approval_outcome, fallback_reason, native_elicitation_attempted, and native_elicitation_completed. Approval is not evidence of successful submission. After approval_status, inspect both status and the nested submission result; failed or declined is not a successful submission. Do not poll indefinitely. Continue to use orders directly for transaction requests.",
		ordersSchema,
		ordersOutputSchema,
	)
}

func (*OrdersTool) Handler(manager *kc.Manager) ToolHandler {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := GetArguments(request)
		mode := SafeAssertString(args["mode"], "")
		if err := validateOrderArguments(args, mode); err != nil {
			return NewToolResultError(err.Error()), nil
		}

		switch mode {
		case "approval_status":
			if err := ValidateRequired(args, "approval_id"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			if browser := browserApprovalsFrom(ctx); browser != nil {
				return browser.status(ctx, request, SafeAssertString(args["approval_id"], ""))
			}
			return NewToolResultError("Browser approvals are unavailable on this connection."), nil
		case "permissions":
			if browser := browserApprovalsFrom(ctx); browser != nil {
				return browser.permissionStatus(ctx, request)
			}
			return NewToolResultError("Browser approvals are unavailable on this connection."), nil
		case "list":
			return PaginatedToolHandler(manager, "orders_list", func(client *kiteconnect.Client) ([]interface{}, error) {
				orders, err := client.GetOrders()
				if err != nil {
					return nil, err
				}
				result := make([]interface{}, len(orders))
				for i, o := range orders {
					result[i] = o
				}
				return result, nil
			})(ctx, request)

		case "history":
			if err := ValidateRequired(args, "order_id"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			orderID := SafeAssertString(args["order_id"], "")
			return handler.WithKiteClientContext(ctx, request, "orders_history", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				history, err := client.GetOrderHistory(orderID)
				if err != nil {
					return NewToolResultError("Failed to get order history"), nil
				}
				return handler.MarshalResponse(history, "orders_history")
			})

		case "trades":
			if err := ValidateRequired(args, "order_id"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			orderID := SafeAssertString(args["order_id"], "")
			return handler.WithKiteClientContext(ctx, request, "orders_trades", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				trades, err := client.GetOrderTrades(orderID)
				if err != nil {
					return NewToolResultError("Failed to get order trades"), nil
				}
				return handler.MarshalResponse(trades, "orders_trades")
			})

		case "all_trades":
			return PaginatedToolHandler(manager, "orders_all_trades", func(client *kiteconnect.Client) ([]interface{}, error) {
				trades, err := client.GetTrades()
				if err != nil {
					return nil, err
				}
				result := make([]interface{}, len(trades))
				for i, t := range trades {
					result[i] = t
				}
				return result, nil
			})(ctx, request)

		case "place":
			if err := ValidateRequired(args, "exchange", "tradingsymbol", "transaction_type", "quantity", "product", "order_type"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			if err := validateOrderIntegers(args); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			variety := SafeAssertString(args["variety"], "regular")
			orderParams := kiteconnect.OrderParams{
				Exchange:          SafeAssertString(args["exchange"], "NSE"),
				Tradingsymbol:     SafeAssertString(args["tradingsymbol"], ""),
				Validity:          SafeAssertString(args["validity"], "DAY"),
				ValidityTTL:       SafeAssertInt(args["validity_ttl"], 0),
				Product:           SafeAssertString(args["product"], ""),
				OrderType:         SafeAssertString(args["order_type"], ""),
				TransactionType:   SafeAssertString(args["transaction_type"], ""),
				Quantity:          SafeAssertInt(args["quantity"], 1),
				DisclosedQuantity: SafeAssertInt(args["disclosed_quantity"], 0),
				Price:             SafeAssertFloat64(args["price"], 0.0),
				TriggerPrice:      SafeAssertFloat64(args["trigger_price"], 0.0),
				IcebergLegs:       SafeAssertInt(args["iceberg_legs"], 0),
				IcebergQty:        SafeAssertInt(args["iceberg_quantity"], 0),
				Tag:               SafeAssertString(args["tag"], ""),
				MarketProtection:  SafeAssertFloat64(args["market_protection"], 0.0),
				Autoslice:         SafeAssertBool(args["autoslice"], false),
			}
			return handler.WithKiteClientContext(ctx, request, "orders_place", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				return withTradeApproval(ctx, request, "orders_place", map[string]any{"variety": variety, "parameters": approvalOrderParams{&orderParams}}, func() (*mcp.CallToolResult, error) {
					resp, err := client.PlaceOrder(variety, orderParams)
					if err != nil {
						handler.manager.Logger.Error("Failed to place order", "error", err)
						return NewToolResultError("Kite API failed to place order: " + err.Error()), nil
					}
					return handler.MarshalResponse(resp, "orders_place")
				})
			})

		case "modify":
			if err := ValidateRequired(args, "order_id", "order_type"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			if err := validateOrderIntegers(args); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			variety := SafeAssertString(args["variety"], "regular")
			orderID := SafeAssertString(args["order_id"], "")
			// Only include fields explicitly provided by the caller.
			// gokiteconnect uses omitempty, so zero values are not sent.
			// This prevents unintended mutations (e.g., quantity defaulting to 1).
			orderParams := kiteconnect.OrderParams{
				OrderType: SafeAssertString(args["order_type"], ""),
			}
			if _, ok := args["quantity"]; ok {
				orderParams.Quantity = SafeAssertInt(args["quantity"], 0)
			}
			if _, ok := args["price"]; ok {
				orderParams.Price = SafeAssertFloat64(args["price"], 0.0)
			}
			if _, ok := args["trigger_price"]; ok {
				orderParams.TriggerPrice = SafeAssertFloat64(args["trigger_price"], 0.0)
			}
			if _, ok := args["validity"]; ok {
				orderParams.Validity = SafeAssertString(args["validity"], "")
			}
			if _, ok := args["validity_ttl"]; ok {
				orderParams.ValidityTTL = SafeAssertInt(args["validity_ttl"], 0)
			}
			if _, ok := args["disclosed_quantity"]; ok {
				orderParams.DisclosedQuantity = SafeAssertInt(args["disclosed_quantity"], 0)
			}
			return handler.WithKiteClientContext(ctx, request, "orders_modify", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				history, err := client.GetOrderHistory(orderID)
				if err != nil {
					return NewToolResultError("Kite API failed to load order for approval: " + err.Error()), nil
				}
				if len(history) == 0 {
					return approvalFailure("missing_order", "Cannot approve an order without its current details."), nil
				}
				currentOrder := history[len(history)-1]

				return withTradeApproval(ctx, request, "orders_modify", map[string]any{"variety": variety, "order_id": orderID, "current_order": currentOrder, "changes": approvalOrderParams{&orderParams}}, func() (*mcp.CallToolResult, error) {
					resp, err := client.ModifyOrder(variety, orderID, orderParams)
					if err != nil {
						handler.manager.Logger.Error("Failed to modify order", "error", err)
						return NewToolResultError("Kite API failed to modify order: " + err.Error()), nil
					}
					return handler.MarshalResponse(resp, "orders_modify")
				})
			})

		case "cancel":
			if err := ValidateRequired(args, "order_id"); err != nil {
				return NewToolResultError(err.Error()), nil
			}
			variety := SafeAssertString(args["variety"], "regular")
			orderID := SafeAssertString(args["order_id"], "")
			return handler.WithKiteClientContext(ctx, request, "orders_cancel", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
				history, err := client.GetOrderHistory(orderID)
				if err != nil {
					return NewToolResultError("Kite API failed to load order for approval: " + err.Error()), nil
				}
				if len(history) == 0 {
					return approvalFailure("missing_order", "Cannot approve an order without its current details."), nil
				}
				currentOrder := history[len(history)-1]

				return withTradeApproval(ctx, request, "orders_cancel", map[string]any{"variety": variety, "order_id": orderID, "current_order": currentOrder}, func() (*mcp.CallToolResult, error) {
					resp, err := client.CancelOrder(variety, orderID, nil)
					if err != nil {
						handler.manager.Logger.Error("Failed to cancel order", "error", err)
						return NewToolResultError("Kite API failed to cancel order: " + err.Error()), nil
					}
					return handler.MarshalResponse(resp, "orders_cancel")
				})
			})

		default:
			return NewToolResultError("Invalid mode. Must be one of: list, history, trades, all_trades, place, modify, cancel, approval_status, permissions"), nil
		}
	}
}

// Validate supplied integer fields before SafeAssertInt can truncate or default
// them. The raw SDK tool handler does not enforce the advertised input schema.
func validateOrderIntegers(args map[string]interface{}) error {
	for _, field := range []struct {
		name string
		min  int
	}{
		{"quantity", 1},
		{"validity_ttl", 1},
		{"disclosed_quantity", 0},
		{"iceberg_legs", 1},
		{"iceberg_quantity", 1},
	} {
		raw, exists := args[field.name]
		if !exists {
			continue
		}
		var value float64
		switch v := raw.(type) {
		case int:
			value = float64(v)
		case float64:
			value = v
		default:
			return ValidationError{field.name, "must be an integer in the supported range"}
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < float64(field.min) || value > 2147483647 {
			return ValidationError{field.name, "must be an integer in the supported range"}
		}
	}
	if args["validity"] == "TTL" {
		if _, exists := args["validity_ttl"]; !exists {
			return ValidationError{"validity_ttl", "is required when validity is TTL"}
		}
	}
	return nil
}
