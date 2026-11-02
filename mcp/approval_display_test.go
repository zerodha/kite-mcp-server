package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

func TestTradeApprovalReadableSummary(t *testing.T) {
	params := kiteconnect.OrderParams{Exchange: "NSE", Tradingsymbol: "INFY", TransactionType: "BUY", Quantity: 1, OrderType: "MARKET", Product: "CNC", Validity: "DAY", MarketProtection: 2.5, Autoslice: true, Tag: "test\nQuantity: 500"}
	snapshot, err := json.Marshal(map[string]any{"variety": "regular", "parameters": approvalOrderParams{&params}})
	require.NoError(t, err)
	form := tradeApprovalForm("orders_place", snapshot)
	for _, detail := range []string{"Place order", "Exchange: NSE", "Symbol: INFY", "Side: BUY", "Quantity: 1", "Order type: MARKET", "Product: CNC", "Validity: DAY", "Variety: regular", "Market protection: 2.5", "Autoslice: true", `Tag: "test\nQuantity: 500"`, "Execution price is set by the market."} {
		require.Contains(t, form.Message, detail)
	}
	require.NotContains(t, form.Message, "orders_place")
	require.NotContains(t, form.Message, `"parameters"`)
	require.NotContains(t, form.Message, "\nQuantity: 500")
	var schema map[string]any
	schemaJSON, err := json.Marshal(form.RequestedSchema)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	confirm := schema["properties"].(map[string]any)["confirm"].(map[string]any)
	require.Equal(t, "Place this order", confirm["title"])
	require.NotContains(t, confirm, "default", "confirmation must not be preselected")
	require.Equal(t, []any{"confirm"}, schema["required"])
}

func TestTradeApprovalSummaryKeepsAllPlacementFields(t *testing.T) {
	fields := map[string]any{"exchange": "NSE", "tradingsymbol": "INFY", "transaction_type": "SELL", "quantity": "100", "order_type": "SL", "product": "CNC", "price": "1500.125", "trigger_price": "1501", "validity": "TTL", "validity_ttl": "10", "disclosed_quantity": "20", "iceberg_legs": "2", "iceberg_quantity": "50", "new_api_field": "9007199254740993"}
	snapshot, err := json.Marshal(map[string]any{"parameters": fields})
	require.NoError(t, err)
	form := tradeApprovalForm("orders_place", snapshot)
	for _, value := range fields {
		require.Contains(t, form.Message, value)
	}
	require.Contains(t, form.Message, "New api field: 9007199254740993")
	require.NotContains(t, form.Message, "Execution price is set by the market.")
}

func TestTradeApprovalHiddenBookkeepingStillChecked(t *testing.T) {
	details := map[string]any{"order_id": "123", "current_order": map[string]any{"tradingsymbol": "INFY", "status": "OPEN", "filled_quantity": 1, "pending_quantity": 2, "account_id": "account-a"}, "changes": map[string]any{"quantity": "3"}}
	executed := false
	result, err := elicitTradeApproval(context.Background(), time.Second, func(_ context.Context, form *sdk.ElicitParams) (*sdk.ElicitResult, error) {
		for _, text := range []string{"Modify order", "Current order:", "Symbol: INFY", "Status: OPEN", "Filled quantity: 1", "Pending quantity: 2", "Requested changes:", "Quantity: 3"} {
			require.Contains(t, form.Message, text)
		}
		require.NotContains(t, form.Message, "account-a")
		details["current_order"].(map[string]any)["account_id"] = "account-b"
		return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
	}, "orders_modify", details, func() (*sdk.CallToolResult, error) { executed = true; return NewToolResultText("executed"), nil })
	require.NoError(t, err)
	require.False(t, executed)
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "parameters_changed")
}

func TestTradeApprovalResponseGuidance(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *sdk.ElicitResult
		guidance bool
	}{
		{"missing", nil, true},
		{"invalid action", &sdk.ElicitResult{Action: "unknown"}, true},
		{"missing confirm", &sdk.ElicitResult{Action: "accept"}, true},
		{"invalid confirm", &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": "true"}}, true},
		{"decline", &sdk.ElicitResult{Action: "decline"}, false},
		{"cancel", &sdk.ElicitResult{Action: "cancel"}, false},
		{"no", &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": false}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := validateTradeApproval(tc.response)
			require.True(t, result.IsError)
			message := result.Content[0].(*sdk.TextContent).Text
			require.Contains(t, message, "no transaction was executed")
			require.Equal(t, tc.guidance, strings.Contains(message, "approval_method=browser"))
			if tc.guidance {
				require.Contains(t, message, orderApprovalHelpURL)
				require.Contains(t, message, "ChatGPT web")
			}
		})
	}
}

func TestApprovalDisplayQuotesUnicodeLayoutControls(t *testing.T) {
	for _, value := range []string{"INFY\u2028SELL", "INFY\u202eSELL"} {
		snapshot, err := json.Marshal(map[string]any{"tradingsymbol": value})
		require.NoError(t, err)
		form := tradeApprovalForm("orders_place", snapshot)
		require.NotContains(t, form.Message, value)
		require.Contains(t, form.Message, `\u202`)
	}
}
