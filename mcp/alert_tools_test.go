package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

func simpleAlertArgs() map[string]interface{} {
	return map[string]interface{}{
		"name":              "NIFTY above 25000",
		"type":              "simple",
		"lhs_exchange":      "INDICES",
		"lhs_tradingsymbol": "NIFTY 50",
		"operator":          ">=",
		"rhs_type":          "constant",
		"rhs_constant":      25000.0,
	}
}

// TestBuildAlertParams tests validation and mapping of alert arguments into AlertParams
func TestBuildAlertParams(t *testing.T) {
	t.Run("simple constant alert", func(t *testing.T) {
		params, err := buildAlertParams(simpleAlertArgs())
		require.NoError(t, err)
		assert.Equal(t, kiteconnect.AlertParams{
			Name:             "NIFTY above 25000",
			Type:             kiteconnect.AlertTypeSimple,
			LHSExchange:      "INDICES",
			LHSTradingSymbol: "NIFTY 50",
			LHSAttribute:     "LastTradedPrice",
			Operator:         kiteconnect.AlertOperatorGE,
			RHSType:          "constant",
			RHSConstant:      25000,
		}, params)
	})

	t.Run("rhs_constant of zero is accepted", func(t *testing.T) {
		args := simpleAlertArgs()
		args["rhs_constant"] = 0
		params, err := buildAlertParams(args)
		require.NoError(t, err)
		assert.Equal(t, 0.0, params.RHSConstant)
	})

	t.Run("instrument alert", func(t *testing.T) {
		args := simpleAlertArgs()
		delete(args, "rhs_constant")
		args["rhs_type"] = "instrument"
		args["rhs_exchange"] = "NSE"
		args["rhs_tradingsymbol"] = "INFY"
		args["lhs_attribute"] = "LastTradedPrice"

		params, err := buildAlertParams(args)
		require.NoError(t, err)
		assert.Equal(t, "instrument", params.RHSType)
		assert.Equal(t, "NSE", params.RHSExchange)
		assert.Equal(t, "INFY", params.RHSTradingSymbol)
		assert.Equal(t, "LastTradedPrice", params.RHSAttribute)
		assert.Equal(t, 0.0, params.RHSConstant)
	})

	t.Run("ato alert with basket JSON string", func(t *testing.T) {
		args := simpleAlertArgs()
		args["type"] = "ato"
		args["basket"] = `{"name":"my-basket","type":"alert","tags":[],"items":[{"type":"insert","tradingsymbol":"INFY","exchange":"NSE","weight":0,"params":{"transaction_type":"BUY","product":"CNC","order_type":"LIMIT","validity":"DAY","quantity":1,"price":1500,"variety":"regular"}}]}`

		params, err := buildAlertParams(args)
		require.NoError(t, err)
		assert.Equal(t, kiteconnect.AlertTypeATO, params.Type)
		require.NotNil(t, params.Basket)
		assert.Equal(t, "my-basket", params.Basket.Name)
		assert.Equal(t, "alert", params.Basket.Type)
		require.Len(t, params.Basket.Items, 1)
		item := params.Basket.Items[0]
		assert.Equal(t, "insert", item.Type)
		assert.Equal(t, "INFY", item.TradingSymbol)
		assert.Equal(t, "NSE", item.Exchange)
		assert.Equal(t, "BUY", item.Params.TransactionType)
		assert.Equal(t, 1, item.Params.Quantity)
		assert.Equal(t, 1500.0, item.Params.Price)
	})

	t.Run("simple alert ignores basket", func(t *testing.T) {
		args := simpleAlertArgs()
		args["basket"] = `{"items":[{"tradingsymbol":"INFY"}]}`
		params, err := buildAlertParams(args)
		require.NoError(t, err)
		assert.Nil(t, params.Basket)
	})

	t.Run("validation errors", func(t *testing.T) {
		testCases := []struct {
			name      string
			mutate    func(map[string]interface{})
			errSubstr string
		}{
			{"missing name", func(a map[string]interface{}) { delete(a, "name") }, "name"},
			{"empty tradingsymbol", func(a map[string]interface{}) { a["lhs_tradingsymbol"] = "" }, "lhs_tradingsymbol"},
			{"missing operator", func(a map[string]interface{}) { delete(a, "operator") }, "operator"},
			{"invalid operator", func(a map[string]interface{}) { a["operator"] = "!=" }, "operator"},
			{"invalid type", func(a map[string]interface{}) { a["type"] = "foo" }, "type"},
			{"invalid rhs_type", func(a map[string]interface{}) { a["rhs_type"] = "foo" }, "rhs_type"},
			{"constant without rhs_constant", func(a map[string]interface{}) { delete(a, "rhs_constant") }, "rhs_constant"},
			{"non-numeric rhs_constant", func(a map[string]interface{}) { a["rhs_constant"] = "25000" }, "rhs_constant"},
			{"instrument without rhs_tradingsymbol", func(a map[string]interface{}) {
				a["rhs_type"] = "instrument"
				a["rhs_exchange"] = "NSE"
			}, "rhs_tradingsymbol"},
			{"ato without basket", func(a map[string]interface{}) { a["type"] = "ato" }, "basket"},
			{"ato with invalid basket JSON", func(a map[string]interface{}) {
				a["type"] = "ato"
				a["basket"] = "{not json"
			}, "basket"},
			{"ato with empty basket items", func(a map[string]interface{}) {
				a["type"] = "ato"
				a["basket"] = `{"name":"empty","items":[]}`
			}, "basket"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				args := simpleAlertArgs()
				tc.mutate(args)
				_, err := buildAlertParams(args)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errSubstr)
			})
		}
	})
}

// TestAlertTools tests alert tool definitions and registration
func TestAlertTools(t *testing.T) {
	allTools := GetAllTools()
	tools := make(map[string]Tool)
	for _, tool := range allTools {
		tools[tool.Tool().Name] = tool
	}

	expected := map[string][]string{
		"get_alerts":        nil,
		"get_alert":         {"uuid"},
		"get_alert_history": {"uuid"},
		"create_alert":      {"name", "type", "lhs_exchange", "lhs_tradingsymbol", "operator", "rhs_type"},
		"modify_alert":      {"uuid", "name", "type", "lhs_exchange", "lhs_tradingsymbol", "operator", "rhs_type"},
		"delete_alerts":     {"uuids"},
	}

	for name, required := range expected {
		t.Run(name, func(t *testing.T) {
			tool, ok := tools[name]
			require.True(t, ok, "alert tool not registered: %s", name)
			def := tool.Tool()
			assert.NotEmpty(t, def.Description)
			assert.ElementsMatch(t, required, def.InputSchema.Required)
		})
	}

	t.Run("create and modify share alert parameters", func(t *testing.T) {
		create := tools["create_alert"].Tool().InputSchema.Properties
		modify := tools["modify_alert"].Tool().InputSchema.Properties
		for prop := range create {
			assert.Contains(t, modify, prop)
		}
		assert.Contains(t, modify, "uuid")
		assert.NotContains(t, create, "uuid")
	})

	t.Run("write tools can be excluded", func(t *testing.T) {
		excludedSet := parseExcludedTools("create_alert,modify_alert,delete_alerts")
		filtered, _, excluded := filterTools(allTools, excludedSet)
		assert.Equal(t, 3, excluded)
		names := make(map[string]bool)
		for _, tool := range filtered {
			names[tool.Tool().Name] = true
		}
		assert.False(t, names["create_alert"])
		assert.False(t, names["modify_alert"])
		assert.False(t, names["delete_alerts"])
		assert.True(t, names["get_alerts"])
		assert.True(t, names["get_alert"])
		assert.True(t, names["get_alert_history"])
	})
}
