package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestHistoricalDateBounds(t *testing.T) {
	for _, tc := range []struct {
		input string
		end   bool
		want  string
	}{
		{"2026-10-22", false, "2026-10-22 00:00:00 +0530"},
		{"2026-10-22", true, "2026-10-22 23:59:59 +0530"},
		{"2026-10-22 09:15:00", true, "2026-10-22 09:15:00 +0530"},
	} {
		got, err := parseHistoricalDate(tc.input, tc.end)
		require.NoError(t, err)
		require.Equal(t, tc.want, got.Format("2006-01-02 15:04:05 -0700"))
	}
	for _, input := range []string{"2026-02-30", "22-10-2026", "2026-10-22T09:15:00", ""} {
		_, err := parseHistoricalDate(input, false)
		require.Error(t, err)
	}
}

func TestMarketStructuredResponses(t *testing.T) {
	schemaJSON, err := json.Marshal((&MarketTool{}).Definition().OutputSchema)
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	// The negotiated 2025-11-25 Tool schema requires an explicit object root.
	require.Equal(t, "object", schema.Type)
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/instruments/historical/123/day" {
			require.Equal(t, "2026-10-22 00:00:00", r.URL.Query().Get("from"))
			require.Equal(t, "2026-10-22 23:59:59", r.URL.Query().Get("to"))
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"candles":[["2026-10-22T00:00:00+0530",10,12,9,11,100]]}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"NSE:GOOD":{"last_price":123,"timestamp":"2026-10-22 15:30:00","last_trade_time":"2026-10-22 15:29:59"}}}`)
	})
	for _, args := range []map[string]interface{}{
		{"mode": "quote", "instruments": []string{"NSE:GOOD"}},
		{"mode": "ltp", "instruments": []string{"NSE:GOOD"}},
		{"mode": "ohlc", "instruments": []string{"NSE:GOOD"}},
		{"mode": "search", "query": "MISSING"},
		{"mode": "historical", "instrument_token": 123, "interval": "day", "from_date": "2026-10-22", "to_date": "2026-10-22"},
	} {
		t.Run(args["mode"].(string), func(t *testing.T) {
			result := callReadContract(t, &MarketTool{}, manager, args)
			require.False(t, result.IsError)
			require.NotNil(t, result.StructuredContent)
			encoded, err := json.Marshal(result.StructuredContent)
			require.NoError(t, err)
			var value interface{}
			require.NoError(t, json.Unmarshal(encoded, &value))
			// CallToolResult.structuredContent must be an object in 2025-11-25.
			object, ok := value.(map[string]interface{})
			require.True(t, ok, "structuredContent must be a JSON object")
			if args["mode"] == "historical" {
				candles := object["data"].([]interface{})
				require.Len(t, candles, 1)
				require.Equal(t, float64(11), candles[0].(map[string]interface{})["close"])
				require.Equal(t, "Asia/Kolkata", object["meta"].(map[string]interface{})["timezone"])
			}
			require.NoError(t, resolved.Validate(value))
			text, err := json.Marshal(result.Content[0])
			require.NoError(t, err)
			var content struct {
				Text string `json:"text"`
			}
			require.NoError(t, json.Unmarshal(text, &content))
			require.JSONEq(t, content.Text, string(encoded))
			if args["mode"] == "ltp" || args["mode"] == "ohlc" {
				response := readContractJSON(t, result)
				require.Equal(t, false, response["meta"].(map[string]interface{})["as_of_available"])
				require.NotContains(t, response["data"].(map[string]interface{})["NSE:GOOD"], "timestamp")
			}
		})
	}
}

func TestHistoricalEmptyDataIsArray(t *testing.T) {
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"candles":[]}}`)
	})
	result := callReadContract(t, &MarketTool{}, manager, map[string]interface{}{
		"mode": "historical", "instrument_token": 123, "interval": "day", "from_date": "2026-10-22", "to_date": "2026-10-22",
	})
	require.False(t, result.IsError)
	require.Equal(t, []interface{}{}, readContractJSON(t, result)["data"])
}
