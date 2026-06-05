package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

func TestBuildAlertParams(t *testing.T) {
	baseArgs := map[string]interface{}{
		"name":              "NIFTY 50 threshold",
		"type":              "simple",
		"lhs_exchange":      "INDICES",
		"lhs_tradingsymbol": "NIFTY 50",
		"lhs_attribute":     "LastTradedPrice",
		"operator":          ">=",
		"rhs_type":          "constant",
		"rhs_constant":      27000.0,
	}

	t.Run("simple constant alert", func(t *testing.T) {
		params, err := buildAlertParams(baseArgs)
		require.NoError(t, err)
		assert.Equal(t, "NIFTY 50 threshold", params.Name)
		assert.Equal(t, kiteconnect.AlertType("simple"), params.Type)
		assert.Equal(t, "INDICES", params.LHSExchange)
		assert.Equal(t, "NIFTY 50", params.LHSTradingSymbol)
		assert.Equal(t, kiteconnect.AlertOperator(">="), params.Operator)
		assert.Equal(t, "constant", params.RHSType)
		assert.Equal(t, 27000.0, params.RHSConstant)
		assert.Nil(t, params.Basket)
	})

	t.Run("instrument rhs alert", func(t *testing.T) {
		args := cloneArgs(baseArgs)
		args["rhs_type"] = "instrument"
		args["rhs_exchange"] = "NSE"
		args["rhs_tradingsymbol"] = "INFY"
		args["rhs_attribute"] = "LastTradedPrice"

		params, err := buildAlertParams(args)
		require.NoError(t, err)
		assert.Equal(t, "instrument", params.RHSType)
		assert.Equal(t, "NSE", params.RHSExchange)
		assert.Equal(t, "INFY", params.RHSTradingSymbol)
		assert.Equal(t, "LastTradedPrice", params.RHSAttribute)
	})

	t.Run("ato alert decodes basket", func(t *testing.T) {
		args := cloneArgs(baseArgs)
		args["type"] = "ato"
		args["basket"] = `{"name":"alerts-basket","type":"alert","tags":[],"items":[{"type":"insert","tradingsymbol":"RELIANCE","exchange":"NSE","weight":10000,"params":{"transaction_type":"BUY","product":"CNC","order_type":"MARKET","validity":"DAY","quantity":1,"variety":"regular","tags":[]}}]}`

		params, err := buildAlertParams(args)
		require.NoError(t, err)
		require.NotNil(t, params.Basket)
		assert.Equal(t, "alerts-basket", params.Basket.Name)
		require.Len(t, params.Basket.Items, 1)
		assert.Equal(t, "RELIANCE", params.Basket.Items[0].TradingSymbol)
		assert.Equal(t, "BUY", params.Basket.Items[0].Params.TransactionType)
	})

	t.Run("requires rhs instrument fields", func(t *testing.T) {
		args := cloneArgs(baseArgs)
		args["rhs_type"] = "instrument"

		_, err := buildAlertParams(args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rhs_exchange")
	})

	t.Run("requires basket for ato", func(t *testing.T) {
		args := cloneArgs(baseArgs)
		args["type"] = "ato"

		_, err := buildAlertParams(args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "basket")
	})

	t.Run("rejects invalid basket json", func(t *testing.T) {
		args := cloneArgs(baseArgs)
		args["type"] = "ato"
		args["basket"] = "{bad-json"

		_, err := buildAlertParams(args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "valid JSON")
	})
}

func TestAlertToolsAreRegistered(t *testing.T) {
	toolNames := map[string]bool{}
	for _, tool := range GetAllTools() {
		toolNames[tool.Tool().Name] = true
	}

	for _, name := range []string{"get_alerts", "get_alert", "get_alert_history", "create_alert", "modify_alert", "delete_alerts"} {
		assert.True(t, toolNames[name], "expected %s to be registered", name)
	}
}

func cloneArgs(args map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	return cloned
}
