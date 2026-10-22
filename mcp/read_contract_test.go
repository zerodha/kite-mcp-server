package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

func readContractManager(t *testing.T, upstream http.HandlerFunc) *kc.Manager {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := instruments.DefaultUpdateConfig()
	config.EnableScheduler = false
	catalog, err := instruments.New(instruments.Config{Logger: logger, UpdateConfig: config, TestData: map[uint32]*instruments.Instrument{}})
	require.NoError(t, err)
	manager, err := kc.New(kc.Config{APIKey: "key", APISecret: "secret", Logger: logger, Instruments: catalog, KiteBaseURI: server.URL})
	require.NoError(t, err)
	t.Cleanup(manager.Shutdown)
	_, _, err = manager.SessionManager().GetOrCreate("read-contract")
	require.NoError(t, err)
	require.NoError(t, manager.SessionManager().UpdateCredentials("read-contract", &kc.KiteCredentials{UserID: "user", AccessToken: "test-access", ExpiresAt: time.Now().Add(time.Hour)}))
	return manager
}

func callReadContract(t *testing.T, tool Tool, manager *kc.Manager, args map[string]interface{}) *mcpsdk.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	result, err := tool.Handler(manager)(context.Background(), &mcpsdk.CallToolRequest{
		Params: &mcpsdk.CallToolParamsRaw{Name: tool.Definition().Name, Arguments: raw},
		Extra:  &mcpsdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"read-contract"}}},
	})
	require.NoError(t, err)
	return result
}

func readContractJSON(t *testing.T, result *mcpsdk.CallToolResult) map[string]interface{} {
	t.Helper()
	require.NotEmpty(t, result.Content)
	content, ok := result.Content[0].(*mcpsdk.TextContent)
	require.True(t, ok)
	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(content.Text), &response))
	return response
}

func TestStrictPaginationArguments(t *testing.T) {
	params, err := ParsePaginationParams(nil)
	require.NoError(t, err)
	require.Equal(t, PaginationParams{Limit: 100}, params)
	for _, tc := range []struct {
		key   string
		value interface{}
	}{
		{"from", -1}, {"from", 0.5}, {"from", "1"}, {"from", nil}, {"from", true},
		{"from", float64(MaxPaginationOffset) + 1}, {"from", 1e100},
		{"limit", 0}, {"limit", -1}, {"limit", 1.5}, {"limit", 501}, {"limit", "5"}, {"limit", nil}, {"limit", false},
		{"limit", math.NaN()}, {"from", math.Inf(1)},
	} {
		t.Run(fmt.Sprintf("%s=%v", tc.key, tc.value), func(t *testing.T) {
			_, err := ParsePaginationParams(map[string]interface{}{tc.key: tc.value})
			require.ErrorContains(t, err, tc.key)
		})
	}
}

func TestPaginatedModesRejectInvalidInputBeforeUpstream(t *testing.T) {
	var calls atomic.Int32
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
	for _, tc := range []struct {
		tool       Tool
		mode, kind string
	}{
		{&PortfolioTool{}, "holdings", "full"}, {&PortfolioTool{}, "holdings", "compact"}, {&PortfolioTool{}, "positions", ""},
		{&OrdersTool{}, "list", ""}, {&OrdersTool{}, "all_trades", ""}, {&GTTTool{}, "list", ""}, {&MarketTool{}, "search", ""},
	} {
		for _, invalid := range []map[string]interface{}{{"limit": 0}, {"limit": -1}, {"limit": 501}, {"from": -1}, {"from": 1.5}, {"limit": "3"}, {"limit": nil}} {
			args := map[string]interface{}{"mode": tc.mode, "type": tc.kind, "query": "TEST"}
			for key, value := range invalid {
				args[key] = value
			}
			result := callReadContract(t, tc.tool, manager, args)
			require.True(t, result.IsError, "tool=%s mode=%s args=%v", tc.tool.Definition().Name, tc.mode, invalid)
		}
	}
	require.Zero(t, calls.Load(), "invalid pagination reached Kite")
}

func TestPaginatedListsHaveBoundedConsistentEnvelopes(t *testing.T) {
	rows := make([]map[string]interface{}, 650)
	for i := range rows {
		rows[i] = map[string]interface{}{"tradingsymbol": fmt.Sprintf("TEST%03d", i), "order_id": fmt.Sprint(i)}
	}
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) {
		var data interface{} = rows
		if r.URL.Path == "/portfolio/positions" {
			data = map[string]interface{}{"day": rows, "net": []interface{}{}}
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": data}))
	})
	for _, tc := range []struct {
		tool       Tool
		mode, kind string
	}{
		{&PortfolioTool{}, "holdings", "full"}, {&PortfolioTool{}, "holdings", "compact"}, {&PortfolioTool{}, "positions", ""},
		{&OrdersTool{}, "list", ""}, {&OrdersTool{}, "all_trades", ""}, {&GTTTool{}, "list", ""},
	} {
		for _, page := range []struct{ from, limit, want int }{{0, 0, 100}, {100, 500, 500}, {650, 5, 0}, {900, 5, 0}} {
			args := map[string]interface{}{"mode": tc.mode, "type": tc.kind, "from": page.from}
			if page.limit != 0 {
				args["limit"] = page.limit
			}
			result := callReadContract(t, tc.tool, manager, args)
			require.False(t, result.IsError)
			response := readContractJSON(t, result)
			require.Len(t, response["data"], page.want)
			pagination := response["pagination"].(map[string]interface{})
			require.Equal(t, float64(page.from), pagination["from"])
			require.Equal(t, float64(page.want), pagination["returned"])
			require.Equal(t, float64(650), pagination["total"])
			require.Equal(t, page.from+page.want < 650, pagination["has_more"])
			if page.limit == 0 {
				require.Equal(t, float64(100), pagination["limit"])
			}
		}
	}
}

func TestSearchPagesAreStableAndBounded(t *testing.T) {
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) { t.Error("search should not call Kite") })
	fixtures := make(map[uint32]*instruments.Instrument)
	for i := 0; i < 650; i++ {
		token := uint32(1000 + i)
		fixtures[token] = &instruments.Instrument{ID: fmt.Sprintf("NSE:TEST%03d", i), InstrumentToken: token, Exchange: "NSE", Name: "TEST", ISIN: "TEST-ISIN", Tradingsymbol: fmt.Sprintf("TEST%03d", i)}
	}
	for i := 0; i < 10; i++ {
		token := uint32(2000 + i)
		fixtures[token] = &instruments.Instrument{ID: fmt.Sprintf("NFO:TEST%03d", i), InstrumentToken: token, Exchange: "NFO", Name: "TEST"}
	}
	manager.Instruments.LoadMap(fixtures)
	for _, args := range []map[string]interface{}{
		{"mode": "search", "query": "NSE:TEST", "limit": 5, "from": 5},
		{"mode": "search", "search_mode": "get_by_isin", "isin": "TEST-ISIN", "limit": 5, "from": 5},
		{"mode": "search", "query": "NFO:TEST", "filter_on": "underlying", "limit": 5, "from": 5},
	} {
		var previous []string
		for i := 0; i < 20; i++ {
			result := callReadContract(t, &MarketTool{}, manager, args)
			require.False(t, result.IsError)
			response := readContractJSON(t, result)
			rows := response["data"].([]interface{})
			ids := make([]string, len(rows))
			for index, row := range rows {
				ids[index] = row.(map[string]interface{})["id"].(string)
			}
			require.Len(t, ids, 5)
			require.True(t, slices.IsSorted(ids))
			if previous != nil {
				require.Equal(t, previous, ids)
			}
			previous = ids
		}
	}
	for _, limit := range []int{0, 500} {
		args := map[string]interface{}{"mode": "search", "query": "NSE:TEST"}
		want := 100
		if limit > 0 {
			args["limit"] = limit
			want = limit
		}
		response := readContractJSON(t, callReadContract(t, &MarketTool{}, manager, args))
		require.Len(t, response["data"], want)
		require.Equal(t, float64(650), response["pagination"].(map[string]interface{})["total"])
	}
}

func TestMissingMarketSymbolsAreExplicit(t *testing.T) {
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"NSE:GOOD":{"last_price":123},"NSE:UNREQUESTED":{"last_price":456}}}`)
	})
	for _, mode := range []string{"quote", "ltp", "ohlc"} {
		for _, symbols := range [][]string{{"NSE:GOOD"}, {"NSE:GOOD", "NSE:MISSING"}, {"NSE:MISSING"}} {
			result := callReadContract(t, &MarketTool{}, manager, map[string]interface{}{"mode": mode, "instruments": symbols})
			response := readContractJSON(t, result)
			data := response["data"].(map[string]interface{})
			meta := response["meta"].(map[string]interface{})
			require.NotContains(t, data, "NSE:UNREQUESTED")
			require.Equal(t, float64(len(symbols)), meta["requested_count"])
			missing := slices.Contains(symbols, "NSE:MISSING")
			wantMissing := 0
			if missing {
				wantMissing = 1
			}
			require.Len(t, meta["missing_instruments"], wantMissing)
			require.Equal(t, len(data) == 0, result.IsError)
			require.Equal(t, missing && len(data) > 0, meta["partial"])
			if len(data) == 0 {
				require.Contains(t, response, "error")
			}
		}
	}
}

func TestMalformedMarketRequestsNeverReachUpstream(t *testing.T) {
	var calls atomic.Int32
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
	for _, mode := range []string{"quote", "ltp", "ohlc"} {
		for _, raw := range []interface{}{nil, "NSE:INFY", []interface{}{}, []interface{}{42}, []interface{}{""}, make([]string, 501)} {
			result := callReadContract(t, &MarketTool{}, manager, map[string]interface{}{"mode": mode, "instruments": raw})
			require.True(t, result.IsError)
		}
	}
	result := callReadContract(t, &MarketTool{}, manager, map[string]interface{}{"mode": "historical", "instrument_token": 408065, "interval": "day", "from_date": "2026-10-23 15:30:00", "to_date": "2026-10-22 09:15:00"})
	require.True(t, result.IsError)
	require.Contains(t, result.Content[0].(*mcpsdk.TextContent).Text, "from_date must be earlier than or equal to to_date")
	require.Zero(t, calls.Load())
}

func TestEnrichmentMetadataMatchesFieldsAndMissingQuotesPreserveBOD(t *testing.T) {
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"NSE:FOUND":{"last_price":123,"lower_circuit_limit":100,"upper_circuit_limit":150}}}`)
	})
	manager.Instruments.LoadMap(map[uint32]*instruments.Instrument{
		1: {ID: "NSE:FOUND", InstrumentToken: 1, LastPrice: 99},
		2: {ID: "NSE:MISSING", InstrumentToken: 2, LastPrice: 98, UpperCircuitLimit: 120},
	})
	for _, verbosity := range []string{"compact", "full"} {
		response := readContractJSON(t, callReadContract(t, &MarketTool{}, manager, map[string]interface{}{"mode": "search", "query": "NSE:", "verbosity": verbosity, "include_live_quote": true}))
		rows := response["data"].([]interface{})
		found := rows[0].(map[string]interface{})
		missing := rows[1].(map[string]interface{})
		meta := response["meta"].(map[string]interface{})
		require.Equal(t, true, meta["live_quote_enriched"])
		require.Equal(t, []interface{}{"NSE:MISSING"}, meta["missing_live_quotes"])
		for _, field := range meta["live_quote_fields"].([]interface{}) {
			require.Contains(t, found, field.(string))
		}
		require.Contains(t, meta["live_quote_fields"], "quote_timestamp")
		require.NotContains(t, meta["live_quote_fields"], "timestamp")
		require.NotContains(t, missing, "quote_timestamp")
		if verbosity == "full" {
			require.Equal(t, float64(98), missing["last_price"])
			require.Equal(t, float64(120), missing["upper_circuit_limit"])
		}
	}
	response := readContractJSON(t, callReadContract(t, &MarketTool{}, manager, map[string]interface{}{"mode": "search", "search_mode": "get_by_id", "id": "NSE:MISSING", "include_live_quote": true}))
	meta := response["meta"].(map[string]interface{})
	require.Equal(t, false, meta["live_quote_enriched"])
	require.Empty(t, meta["live_quote_fields"])
}
