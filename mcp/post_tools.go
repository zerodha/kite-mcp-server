package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
)

type PlaceOrderTool struct{}

func (*PlaceOrderTool) Tool() mcp.Tool {
	return mcp.NewTool("place_order",
		mcp.WithDescription("Place an order"),
		mcp.WithString("variety",
			mcp.Description("Order variety"),
			mcp.Required(),
			mcp.DefaultString("regular"),
			mcp.Enum("regular", "co", "amo", "iceberg", "auction"),
		),
		mcp.WithString("exchange",
			mcp.Description("The exchange to which the order should be placed"),
			mcp.Required(),
			mcp.DefaultString("NSE"),
			mcp.Enum("NSE", "BSE", "MCX", "NFO", "BFO"),
		),
		mcp.WithString("tradingsymbol",
			mcp.Description("Trading symbol"),
			mcp.Required(),
		),
		mcp.WithString("transaction_type",
			mcp.Description("Transaction type"),
			mcp.Required(),
			mcp.Enum("BUY", "SELL"),
		),
		mcp.WithNumber("quantity",
			mcp.Description("Quantity"),
			mcp.Required(),
			mcp.DefaultString("1"),
			mcp.Min(1),
		),
		mcp.WithString("product",
			mcp.Description("Product type"),
			mcp.Required(),
			mcp.Enum("CNC", "NRML", "MIS", "MTF"),
		),
		mcp.WithString("order_type",
			mcp.Description("Order type"),
			mcp.Required(),
			mcp.Enum("MARKET", "LIMIT", "SL", "SL-M"),
		),
		mcp.WithNumber("price",
			mcp.Description("Price (required for LIMIT order_type"),
		),
		mcp.WithString("validity",
			mcp.Description("Order Validity. (DAY for regular orders, IOC for immediate or cancel, and TTL for orders valid for specific minutes"),
			mcp.Enum("DAY", "IOC", "TTL"),
		),
		mcp.WithNumber("validity_ttl",
			mcp.Description("Order life span in minutes for TTL validity orders, required for TTL orders"),
		),
		mcp.WithNumber("disclosed_quantity",
			mcp.Description("Quantity to disclose publicly (for equity trades)"),
		),
		mcp.WithNumber("trigger_price",
			mcp.Description("The price at which an order should be triggered (SL, SL-M orders)"),
		),
		mcp.WithNumber("iceberg_legs",
			mcp.Description("Number of legs for iceberg orders"),
		),
		mcp.WithNumber("iceberg_quantity",
			mcp.Description("Quantity per leg for iceberg orders"),
		),
		mcp.WithString("tag",
			mcp.Description("An optional tag to apply to an order to identify it (alphanumeric, max 20 chars)"),
			mcp.MaxLength(20),
		),
	)
}

func (*PlaceOrderTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "variety", "exchange", "tradingsymbol", "transaction_type", "quantity", "product", "order_type"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		variety := SafeAssertString(args["variety"], "regular")
		orderParams := kiteconnect.OrderParams{
			Exchange:          SafeAssertString(args["exchange"], "NSE"),
			Tradingsymbol:     SafeAssertString(args["tradingsymbol"], ""),
			Validity:          SafeAssertString(args["validity"], ""),
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
		}
		return handler.WithKiteClient(ctx, "place_order", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			resp, err := client.PlaceOrder(variety, orderParams)
			if err != nil {
				handler.manager.Logger.Error("Failed to place order", "error", err)
				return mcp.NewToolResultError("Failed to place order"), nil
			}
			return handler.MarshalResponse(resp, "place_order")
		})
	}
}

type ModifyOrderTool struct{}

func (*ModifyOrderTool) Tool() mcp.Tool {
	return mcp.NewTool("modify_order",
		mcp.WithDescription("Modify an existing order"),
		mcp.WithString("variety",
			mcp.Description("Order variety"),
			mcp.Required(),
			mcp.DefaultString("regular"),
			mcp.Enum("regular", "co", "amo", "iceberg", "auction"),
		),
		mcp.WithString("order_id",
			mcp.Description("Order ID"),
			mcp.Required(),
		),
		mcp.WithNumber("quantity",
			mcp.Description("Quantity"),
			mcp.DefaultString("1"),
			mcp.Min(1),
		),
		mcp.WithNumber("price",
			mcp.Description("Price (required for LIMIT order_type"),
		),
		mcp.WithString("order_type",
			mcp.Description("Order type"),
			mcp.Required(),
			mcp.Enum("MARKET", "LIMIT", "SL", "SL-M"),
		),
		mcp.WithNumber("trigger_price",
			mcp.Description("The price at which an order should be triggered (SL, SL-M orders)"),
		),
		mcp.WithString("validity",
			mcp.Description("Order Validity. (DAY for regular orders, IOC for immediate or cancel, and TTL for orders valid for specific minutes"),
			mcp.Enum("DAY", "IOC", "TTL"),
		),
		mcp.WithNumber("disclosed_quantity",
			mcp.Description("Quantity to disclose publicly (for equity trades)"),
		),
	)
}

func (*ModifyOrderTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "variety", "order_id", "order_type"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		variety := SafeAssertString(args["variety"], "regular")
		orderID := SafeAssertString(args["order_id"], "")
		orderParams := kiteconnect.OrderParams{
			Quantity:          SafeAssertInt(args["quantity"], 1),
			Price:             SafeAssertFloat64(args["price"], 0.0),
			OrderType:         SafeAssertString(args["order_type"], ""),
			TriggerPrice:      SafeAssertFloat64(args["trigger_price"], 0.0),
			Validity:          SafeAssertString(args["validity"], ""),
			DisclosedQuantity: SafeAssertInt(args["disclosed_quantity"], 0),
		}
		return handler.WithKiteClient(ctx, "modify_order", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			resp, err := client.ModifyOrder(variety, orderID, orderParams)
			if err != nil {
				handler.manager.Logger.Error("Failed to modify order", "error", err)
				return mcp.NewToolResultError("Failed to modify order"), nil
			}
			return handler.MarshalResponse(resp, "modify_order")
		})
	}
}

type CancelOrderTool struct{}

func (*CancelOrderTool) Tool() mcp.Tool {
	return mcp.NewTool("cancel_order",
		mcp.WithDescription("Cancel an existing order"),
		mcp.WithString("variety",
			mcp.Description("Order variety"),
			mcp.Required(),
			mcp.DefaultString("regular"),
			mcp.Enum("regular", "co", "amo", "iceberg", "auction"),
		),
		mcp.WithString("order_id",
			mcp.Description("Order ID"),
			mcp.Required(),
		),
	)
}

func (*CancelOrderTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "variety", "order_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		variety := SafeAssertString(args["variety"], "regular")
		orderID := SafeAssertString(args["order_id"], "")
		return handler.WithKiteClient(ctx, "cancel_order", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			resp, err := client.CancelOrder(variety, orderID, nil)
			if err != nil {
				handler.manager.Logger.Error("Failed to cancel order", "error", err)
				return mcp.NewToolResultError("Failed to cancel order"), nil
			}
			return handler.MarshalResponse(resp, "cancel_order")
		})
	}
}

type PlaceGTTOrderTool struct{}

func (*PlaceGTTOrderTool) Tool() mcp.Tool {
	return mcp.NewTool("place_gtt_order",
		mcp.WithDescription("Place a GTT (Good Till Triggered) order"),
		mcp.WithString("exchange",
			mcp.Description("The exchange to which the order should be placed"),
			mcp.Required(),
			mcp.DefaultString("NSE"),
			mcp.Enum("NSE", "BSE", "MCX", "NFO", "BFO"),
		),
		mcp.WithString("tradingsymbol",
			mcp.Description("Trading symbol"),
			mcp.Required(),
		),
		mcp.WithNumber("last_price",
			mcp.Description("Last price of the instrument"),
			mcp.Required(),
		),
		mcp.WithString("transaction_type",
			mcp.Description("Transaction type"),
			mcp.Required(),
			mcp.Enum("BUY", "SELL"),
		),
		mcp.WithString("product",
			mcp.Description("Product type"),
			mcp.Required(),
			mcp.Enum("CNC", "NRML", "MIS", "MTF"),
		),
		mcp.WithString("trigger_type",
			mcp.Description("GTT trigger type"),
			mcp.Required(),
			mcp.Enum("single", "two-leg"),
		),
		// For single leg trigger
		mcp.WithNumber("trigger_value",
			mcp.Description("Price point at which the GTT will be triggered (for single-leg)"),
		),
		mcp.WithNumber("quantity",
			mcp.Description("Quantity for the order (for single-leg)"),
		),
		mcp.WithNumber("limit_price",
			mcp.Description("Limit price for the order (for single-leg)"),
		),
		// For two-leg trigger
		mcp.WithNumber("upper_trigger_value",
			mcp.Description("Upper price point at which the GTT will be triggered (for two-leg)"),
		),
		mcp.WithNumber("upper_quantity",
			mcp.Description("Quantity for the upper trigger order (for two-leg)"),
		),
		mcp.WithNumber("upper_limit_price",
			mcp.Description("Limit price for the upper trigger order (for two-leg)"),
		),
		mcp.WithNumber("lower_trigger_value",
			mcp.Description("Lower price point at which the GTT will be triggered (for two-leg)"),
		),
		mcp.WithNumber("lower_quantity",
			mcp.Description("Quantity for the lower trigger order (for two-leg)"),
		),
		mcp.WithNumber("lower_limit_price",
			mcp.Description("Limit price for the lower trigger order (for two-leg)"),
		),
	)
}

func (*PlaceGTTOrderTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "exchange", "tradingsymbol", "last_price", "transaction_type", "product", "trigger_type"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
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
			return mcp.NewToolResultError("Invalid trigger_type. Must be 'single' or 'two-leg'"), nil
		}

		return handler.WithKiteClient(ctx, "place_gtt_order", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			resp, err := client.PlaceGTT(gttParams)
			if err != nil {
				handler.manager.Logger.Error("Failed to place GTT order", "error", err)
				return mcp.NewToolResultError("Failed to place GTT order"), nil
			}
			return handler.MarshalResponse(resp, "place_gtt_order")
		})
	}
}

type DeleteGTTOrderTool struct{}

func (*DeleteGTTOrderTool) Tool() mcp.Tool {
	return mcp.NewTool("delete_gtt_order",
		mcp.WithDescription("Delete an existing GTT (Good Till Triggered) order"),
		mcp.WithNumber("trigger_id",
			mcp.Description("The ID of the GTT order to delete"),
			mcp.Required(),
		),
	)
}

func (*DeleteGTTOrderTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "trigger_id"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		triggerID := SafeAssertInt(args["trigger_id"], 0)
		return handler.WithKiteClient(ctx, "delete_gtt_order", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			resp, err := client.DeleteGTT(triggerID)
			if err != nil {
				handler.manager.Logger.Error("Failed to delete GTT order", "error", err)
				return mcp.NewToolResultError("Failed to delete GTT order"), nil
			}
			return handler.MarshalResponse(resp, "delete_gtt_order")
		})
	}
}

type ModifyGTTOrderTool struct{}

func (*ModifyGTTOrderTool) Tool() mcp.Tool {
	return mcp.NewTool("modify_gtt_order",
		mcp.WithDescription("Modify an existing GTT (Good Till Triggered) order"),
		mcp.WithNumber("trigger_id",
			mcp.Description("The ID of the GTT order to modify"),
			mcp.Required(),
		),
		mcp.WithString("exchange",
			mcp.Description("The exchange to which the order should be placed"),
			mcp.Required(),
			mcp.DefaultString("NSE"),
			mcp.Enum("NSE", "BSE", "MCX", "NFO", "BFO"),
		),
		mcp.WithString("tradingsymbol",
			mcp.Description("Trading symbol"),
			mcp.Required(),
		),
		mcp.WithNumber("last_price",
			mcp.Description("Last price of the instrument"),
			mcp.Required(),
		),
		mcp.WithString("transaction_type",
			mcp.Description("Transaction type"),
			mcp.Required(),
			mcp.Enum("BUY", "SELL"),
		),
		mcp.WithString("trigger_type",
			mcp.Description("GTT trigger type"),
			mcp.Required(),
			mcp.Enum("single", "two-leg"),
		),
		// For single leg trigger
		mcp.WithNumber("trigger_value",
			mcp.Description("Price point at which the GTT will be triggered (for single-leg)"),
		),
		mcp.WithNumber("quantity",
			mcp.Description("Quantity for the order (for single-leg)"),
		),
		mcp.WithNumber("limit_price",
			mcp.Description("Limit price for the order (for single-leg)"),
		),
		// For two-leg trigger
		mcp.WithNumber("upper_trigger_value",
			mcp.Description("Upper price point at which the GTT will be triggered (for two-leg)"),
		),
		mcp.WithNumber("upper_quantity",
			mcp.Description("Quantity for the upper trigger order (for two-leg)"),
		),
		mcp.WithNumber("upper_limit_price",
			mcp.Description("Limit price for the upper trigger order (for two-leg)"),
		),
		mcp.WithNumber("lower_trigger_value",
			mcp.Description("Lower price point at which the GTT will be triggered (for two-leg)"),
		),
		mcp.WithNumber("lower_quantity",
			mcp.Description("Quantity for the lower trigger order (for two-leg)"),
		),
		mcp.WithNumber("lower_limit_price",
			mcp.Description("Limit price for the lower trigger order (for two-leg)"),
		),
	)
}

func (*ModifyGTTOrderTool) Handler(manager *kc.Manager) server.ToolHandlerFunc {
	handler := NewToolHandler(manager)
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if err := ValidateRequired(args, "trigger_id", "exchange", "tradingsymbol", "last_price", "transaction_type", "trigger_type"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		triggerID := SafeAssertInt(args["trigger_id"], 0)
		gttParams := kiteconnect.GTTParams{
			Exchange:        SafeAssertString(args["exchange"], "NSE"),
			Tradingsymbol:   SafeAssertString(args["tradingsymbol"], ""),
			LastPrice:       SafeAssertFloat64(args["last_price"], 0.0),
			TransactionType: SafeAssertString(args["transaction_type"], ""),
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
			return mcp.NewToolResultError("Invalid trigger_type. Must be 'single' or 'two-leg'"), nil
		}
		return handler.WithKiteClient(ctx, "modify_gtt_order", func(client *kiteconnect.Client) (*mcp.CallToolResult, error) {
			resp, err := client.ModifyGTT(triggerID, gttParams)
			if err != nil {
				handler.manager.Logger.Error("Failed to modify GTT order", "error", err)
				return mcp.NewToolResultError("Failed to modify GTT order"), nil
			}
			return handler.MarshalResponse(resp, "modify_gtt_order")
		})
	}
}
