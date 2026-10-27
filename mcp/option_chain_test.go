package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

func optionChainTestManager(t *testing.T, strikes int) *kc.Manager {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := instruments.DefaultUpdateConfig()
	config.EnableScheduler = false
	data := map[uint32]*instruments.Instrument{
		1: {ID: "NSE:NIFTY 50", InstrumentToken: 1, Exchange: "NSE", Segment: "INDICES", Tradingsymbol: "NIFTY 50", Name: "NIFTY 50", InstrumentType: "IND", Active: true},
		2: {ID: "NFO:NIFTY99JANFUT", InstrumentToken: 2, Exchange: "NFO", Segment: "NFO-FUT", Tradingsymbol: "NIFTY99JANFUT", Name: "NIFTY", InstrumentType: "FUT", ExpiryDate: "2099-01-29", ExpiryType: "monthly", Active: true},
	}
	for n := 0; n < strikes; n++ {
		token := uint32(10 + n)
		data[token] = &instruments.Instrument{ID: fmt.Sprintf("NFO:NIFTY99JAN%dCE", 20000+n*50), InstrumentToken: token, Exchange: "NFO", Segment: "NFO-OPT", Tradingsymbol: fmt.Sprintf("NIFTY99JAN%dCE", 20000+n*50), Name: "NIFTY", InstrumentType: "CE", ExpiryDate: "2099-01-29", ExpiryType: "monthly", Strike: float64(20000 + n*50), Active: true}
	}
	catalog, err := instruments.New(instruments.Config{Logger: logger, UpdateConfig: config, TestData: data})
	require.NoError(t, err)
	t.Cleanup(catalog.Shutdown)
	return &kc.Manager{Instruments: catalog, Logger: logger}
}

func TestOptionChainCompleteIndexStrikePagination(t *testing.T) {
	manager := optionChainTestManager(t, 2001)
	args := map[string]interface{}{"mode": "option_chain", "underlying": "NIFTY 50", "limit": 500}
	result := callReadContract(t, &MarketTool{}, manager, args)
	require.False(t, result.IsError)
	first := readContractJSON(t, result)
	data := first["data"].(map[string]interface{})
	meta := first["meta"].(map[string]interface{})
	page := first["pagination"].(map[string]interface{})
	require.Len(t, data["strikes"], 500)
	require.Equal(t, float64(2001), page["total"])
	require.Equal(t, true, page["has_more"])
	require.Equal(t, false, meta["complete"])
	require.Equal(t, true, meta["truncated"])
	require.Equal(t, "NIFTY 50", data["underlying_name"])
	require.Equal(t, "2099-01-29", data["expiry"])
	require.Equal(t, []interface{}{"2099-01-29"}, data["expiries"])
	require.Equal(t, "NSE:NIFTY 50", data["underlying"].(map[string]interface{})["id"])
	require.Equal(t, "NFO:NIFTY99JANFUT", data["future"].(map[string]interface{})["id"])
	rows := data["strikes"].([]interface{})
	require.Nil(t, rows[0].(map[string]interface{})["put"])
	require.Equal(t, false, meta["live_quote_enriched"])
	require.Equal(t, "strikes", meta["pagination_unit"])
	var typed interface{}
	encoded, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &typed))
	require.Equal(t, first, typed)
	schemaJSON, err := json.Marshal((&MarketTool{}).Definition().OutputSchema)
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	require.NoError(t, resolved.Validate(typed))
	args["snapshot"] = meta["catalog_revision"]
	args["expiry"] = "2099-01-29"
	args["from"] = 2000
	last := callReadContract(t, &MarketTool{}, manager, args)
	require.False(t, last.IsError)
	lastJSON := readContractJSON(t, last)
	require.Len(t, lastJSON["data"].(map[string]interface{})["strikes"], 1)
	require.Equal(t, false, lastJSON["pagination"].(map[string]interface{})["has_more"])
	args["snapshot"] = "old-catalog"
	require.True(t, callReadContract(t, &MarketTool{}, manager, args).IsError)
}

func TestOptionChainValidationAndCompleteScope(t *testing.T) {
	manager := optionChainTestManager(t, 2)
	valid := map[string]interface{}{"mode": "option_chain", "underlying": "NIFTY"}
	result := callReadContract(t, &MarketTool{}, manager, valid)
	require.False(t, result.IsError)
	require.Equal(t, true, readContractJSON(t, result)["meta"].(map[string]interface{})["complete"])
	for _, patch := range []map[string]interface{}{
		{"underlying": ""}, {"underlying": 123}, {"underlying": "UNKNOWN"}, {"exchange": "MCX"}, {"exchange": 123}, {"expiry": "2099-02-30"}, {"expiry": ""}, {"expiry": 123}, {"expiry": "2098-01-01"}, {"snapshot": false}, {"limit": 501}, {"from": 0.5},
	} {
		args := map[string]interface{}{"mode": "option_chain", "underlying": "NIFTY"}
		for k, v := range patch {
			args[k] = v
		}
		require.True(t, callReadContract(t, &MarketTool{}, manager, args).IsError, "args=%v", args)
	}
	manager.Instruments = nil
	require.True(t, callReadContract(t, &MarketTool{}, manager, valid).IsError)
}
