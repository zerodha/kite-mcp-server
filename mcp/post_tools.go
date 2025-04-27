package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

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
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		args := request.Params.Arguments

		variety := assertString(args["variety"])
		orderParams := kiteconnect.OrderParams{
			Exchange:          assertString(args["exchange"]),
			Tradingsymbol:     assertString(args["tradingsymbol"]),
			Validity:          assertString(args["validity"]),
			ValidityTTL:       assertInt(args["validity_ttl"]),
			Product:           assertString(args["product"]),
			OrderType:         assertString(args["order_type"]),
			TransactionType:   assertString(args["transaction_type"]),
			Quantity:          assertInt(args["quantity"]),
			DisclosedQuantity: assertInt(args["disclosed_quantity"]),
			Price:             assertFloat64(args["price"]),
			TriggerPrice:      assertFloat64(args["trigger_price"]),
			IcebergLegs:       assertInt(args["iceberg_legs"]),
			IcebergQty:        assertInt(args["iceberg_quantity"]),
			Tag:               assertString(args["tag"]),
		}

		resp, err := kc.Kite.Client.PlaceOrder(variety, orderParams)
		if err != nil {
			log.Println("error getting orders", err)
			return nil, err
		}

		v, err := json.Marshal(resp)
		if err != nil {
			log.Println("error marshalling orders", err)
			return nil, err
		}

		ordersRespJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: ordersRespJSON,
				},
			},
		}, nil
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
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		args := request.Params.Arguments

		variety := assertString(args["variety"])
		orderID := assertString(args["order_id"])

		orderParams := kiteconnect.OrderParams{
			Quantity:          assertInt(args["quantity"]),
			Price:             assertFloat64(args["price"]),
			OrderType:         assertString(args["order_type"]),
			TriggerPrice:      assertFloat64(args["trigger_price"]),
			Validity:          assertString(args["validity"]),
			DisclosedQuantity: assertInt(args["disclosed_quantity"]),
		}

		resp, err := kc.Kite.Client.ModifyOrder(variety, orderID, orderParams)
		if err != nil {
			log.Println("error modifying order", err)
			return nil, err
		}

		v, err := json.Marshal(resp)
		if err != nil {
			log.Println("error marshalling orders", err)
			return nil, err
		}

		ordersRespJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: ordersRespJSON,
				},
			},
		}, nil
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
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		args := request.Params.Arguments

		variety := assertString(args["variety"])
		orderID := assertString(args["order_id"])

		resp, err := kc.Kite.Client.CancelOrder(variety, orderID, nil)
		if err != nil {
			log.Println("error cancelling order", err)
			return nil, err
		}

		v, err := json.Marshal(resp)
		if err != nil {
			log.Println("error marshalling orders", err)
			return nil, err
		}

		ordersRespJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: ordersRespJSON,
				},
			},
		}, nil
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
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		args := request.Params.Arguments

		// Set up basic GTT params
		gttParams := kiteconnect.GTTParams{
			Exchange:        assertString(args["exchange"]),
			Tradingsymbol:   assertString(args["tradingsymbol"]),
			LastPrice:       assertFloat64(args["last_price"]),
			TransactionType: assertString(args["transaction_type"]),
		}

		// Set up trigger based on trigger_type
		triggerType := assertString(args["trigger_type"])

		if triggerType == "single" {
			gttParams.Trigger = &kiteconnect.GTTSingleLegTrigger{
				TriggerParams: kiteconnect.TriggerParams{
					TriggerValue: assertFloat64(args["trigger_value"]),
					Quantity:     assertFloat64(args["quantity"]),
					LimitPrice:   assertFloat64(args["limit_price"]),
				},
			}
		} else if triggerType == "two-leg" {
			gttParams.Trigger = &kiteconnect.GTTOneCancelsOtherTrigger{
				Upper: kiteconnect.TriggerParams{
					TriggerValue: assertFloat64(args["upper_trigger_value"]),
					Quantity:     assertFloat64(args["upper_quantity"]),
					LimitPrice:   assertFloat64(args["upper_limit_price"]),
				},
				Lower: kiteconnect.TriggerParams{
					TriggerValue: assertFloat64(args["lower_trigger_value"]),
					Quantity:     assertFloat64(args["lower_quantity"]),
					LimitPrice:   assertFloat64(args["lower_limit_price"]),
				},
			}
		} else {
			return nil, fmt.Errorf("invalid trigger_type: %s", triggerType)
		}

		// Place GTT order
		resp, err := kc.Kite.Client.PlaceGTT(gttParams)
		if err != nil {
			log.Println("error placing GTT order", err)
			return nil, err
		}

		v, err := json.Marshal(resp)
		if err != nil {
			log.Println("error marshalling GTT response", err)
			return nil, err
		}

		gttRespJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: gttRespJSON,
				},
			},
		}, nil
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
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		args := request.Params.Arguments

		// Get the trigger ID to delete
		triggerID := assertInt(args["trigger_id"])

		// Delete the GTT order
		resp, err := kc.Kite.Client.DeleteGTT(triggerID)
		if err != nil {
			log.Println("error deleting GTT order", err)
			return nil, err
		}

		v, err := json.Marshal(resp)
		if err != nil {
			log.Println("error marshalling GTT deletion response", err)
			return nil, err
		}

		gttRespJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: gttRespJSON,
				},
			},
		}, nil
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
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess := server.ClientSessionFromContext(ctx)

		kc, err := manager.GetSession(sess.SessionID())
		if err != nil {
			log.Println("error getting session", err)
			return nil, err
		}

		args := request.Params.Arguments

		// Get the trigger ID to modify
		triggerID := assertInt(args["trigger_id"])

		// Set up basic GTT params
		gttParams := kiteconnect.GTTParams{
			Exchange:        assertString(args["exchange"]),
			Tradingsymbol:   assertString(args["tradingsymbol"]),
			LastPrice:       assertFloat64(args["last_price"]),
			TransactionType: assertString(args["transaction_type"]),
		}

		// Set up trigger based on trigger_type
		triggerType := assertString(args["trigger_type"])

		if triggerType == "single" {
			gttParams.Trigger = &kiteconnect.GTTSingleLegTrigger{
				TriggerParams: kiteconnect.TriggerParams{
					TriggerValue: assertFloat64(args["trigger_value"]),
					Quantity:     assertFloat64(args["quantity"]),
					LimitPrice:   assertFloat64(args["limit_price"]),
				},
			}
		} else if triggerType == "two-leg" {
			gttParams.Trigger = &kiteconnect.GTTOneCancelsOtherTrigger{
				Upper: kiteconnect.TriggerParams{
					TriggerValue: assertFloat64(args["upper_trigger_value"]),
					Quantity:     assertFloat64(args["upper_quantity"]),
					LimitPrice:   assertFloat64(args["upper_limit_price"]),
				},
				Lower: kiteconnect.TriggerParams{
					TriggerValue: assertFloat64(args["lower_trigger_value"]),
					Quantity:     assertFloat64(args["lower_quantity"]),
					LimitPrice:   assertFloat64(args["lower_limit_price"]),
				},
			}
		} else {
			return nil, fmt.Errorf("invalid trigger_type: %s", triggerType)
		}

		// Modify GTT order
		resp, err := kc.Kite.Client.ModifyGTT(triggerID, gttParams)
		if err != nil {
			log.Println("error modifying GTT order", err)
			return nil, err
		}

		v, err := json.Marshal(resp)
		if err != nil {
			log.Println("error marshalling GTT response", err)
			return nil, err
		}

		gttRespJSON := string(v)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: gttRespJSON,
				},
			},
		}, nil
	}
}
