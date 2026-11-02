package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestModernHTTPDisconnectCancelsToolHandler(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	server := NewServer(&sdk.Implementation{Name: "test", Version: "1"})
	server.AddTool(NewTool("wait", "Wait for cancellation", nil), func(ctx context.Context, _ *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(stopped)
		case <-time.After(5 * time.Second):
		}
		return NewToolResultText("done"), nil
	})
	endpoint := httptest.NewServer(NewHTTPHandler(server, nil))
	defer endpoint.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wait","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "wait")
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := http.DefaultClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("tool handler did not start")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("modern HTTP disconnect did not cancel tool handler")
	}
	<-done
}

func TestMalformedTradingInputsDoNotReachKite(t *testing.T) {
	var calls atomic.Int32
	manager := approvalTestManager(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); http.Error(w, "unexpected request", 500) })
	cases := []struct {
		tool  Tool
		args  map[string]any
		field string
	}{
		{&GTTTool{}, map[string]any{"mode": "delete", "trigger_id": 123.9}, "trigger_id"},
		{&GTTTool{}, map[string]any{"mode": "delete", "trigger_id": "123"}, "trigger_id"},
		{&GTTTool{}, map[string]any{"mode": "delete", "trigger_id": -1}, "trigger_id"},
		{&OrdersTool{}, map[string]any{"mode": "history", "order_id": "../user/profile"}, "order_id"},
		{&OrdersTool{}, map[string]any{"mode": "trades", "order_id": "order-1?other=1"}, "order_id"},
		{&OrdersTool{}, map[string]any{"mode": "cancel", "order_id": "order-1", "variety": "../alerts"}, "variety"},
		{&OrdersTool{}, map[string]any{"mode": "place", "market_protection": "2.5"}, "market_protection"},
		{&OrdersTool{}, map[string]any{"mode": "place", "market_protection": nil}, "market_protection"},
		{&OrdersTool{}, map[string]any{"mode": "place", "market_protection": -0.5}, "market_protection"},
		{&OrdersTool{}, map[string]any{"mode": "place", "price": "1500"}, "price"},
		{&OrdersTool{}, map[string]any{"mode": "place", "autoslice": "yes"}, "autoslice"},
		{&AlertsTool{}, map[string]any{"mode": "modify", "uuids": []string{"../orders/regular/order-1"}}, "uuids"},
		{&AlertsTool{}, map[string]any{"mode": "modify", "uuids": []string{"a", "b"}}, "Exactly one UUID"},
		{&AlertsTool{}, map[string]any{"mode": "get", "uuids": []any{123}}, "uuids"},
		{&AlertsTool{}, map[string]any{"mode": "get", "uuids": "a"}, "uuids"},
		{&MarketTool{}, map[string]any{"mode": "historical", "instrument_token": 123.9, "interval": "day", "from_date": "2026-01-01", "to_date": "2026-01-02"}, "instrument_token"},
		{&MarketTool{}, map[string]any{"mode": "historical", "instrument_token": 123, "interval": "../../user/profile", "from_date": "2026-01-01", "to_date": "2026-01-02"}, "interval"},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(tc.args)
		require.NoError(t, err)
		result, err := tc.tool.Handler(manager)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: raw}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"approval-test"}}}})
		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Contains(t, result.Content[0].(*sdk.TextContent).Text, tc.field)
	}
	require.Zero(t, calls.Load())
}

func TestAlertMutationHonorsCancellation(t *testing.T) {
	for _, mode := range []string{"create", "modify", "delete"} {
		for _, when := range []string{"before request", "during request"} {
			t.Run(mode+"/"+when, func(t *testing.T) {
				var calls atomic.Int32
				started, release := make(chan struct{}), make(chan struct{})
				manager := approvalTestManager(t, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					close(started)
					select {
					case <-r.Context().Done():
					case <-release:
					}
				})
				defer close(release)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				args, _ := json.Marshal(map[string]any{"mode": mode, "uuids": []string{"alert-1"}, "name": "test", "alert_type": "simple", "lhs_exchange": "NSE", "lhs_tradingsymbol": "INFY", "lhs_attribute": "LastTradedPrice", "operator": ">", "rhs_type": "constant", "rhs_constant": 1500})
				req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: args}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"approval-test"}}}}
				if when == "before request" {
					cancel()
				}
				done := make(chan *sdk.CallToolResult, 1)
				go func() { result, _ := (&AlertsTool{}).Handler(manager)(ctx, req); done <- result }()
				if when == "during request" {
					select {
					case <-started:
						cancel()
					case <-time.After(3 * time.Second):
						t.Fatal("alert mutation did not start")
					}
				}
				select {
				case result := <-done:
					require.NotNil(t, result)
					require.True(t, result.IsError)
				case <-time.After(2 * time.Second):
					t.Fatal("alert mutation ignored cancellation")
				}
				if when == "before request" {
					require.Zero(t, calls.Load())
				} else {
					require.EqualValues(t, 1, calls.Load())
				}
			})
		}
	}
}

func TestSearchRejectsUnboundedLabelsBeforeMetrics(t *testing.T) {
	// A nil manager proves validation does not first access metrics or catalogs.
	for _, args := range []map[string]any{
		{"mode": "search", "search_mode": "attacker-value", "query": "INFY"},
		{"mode": "search", "verbosity": "attacker-value", "query": "INFY"},
		{"mode": "search", "query": strings.Repeat("x", 201)},
		{"mode": "search", "query": "INFY", "inst_token": 123.5},
	} {
		raw, err := json.Marshal(args)
		require.NoError(t, err)
		result, err := (&MarketTool{}).Handler(nil)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: raw}})
		require.NoError(t, err)
		require.True(t, result.IsError)
	}
}

func TestMalformedTradingTermsCannotDefaultToZero(t *testing.T) {
	for _, value := range []any{nil, "100", true} {
		_, err := buildGTTParams(map[string]any{"quantity": value, "trigger_type": "single"})
		require.Error(t, err)
		err = validateAlertParams(map[string]any{"name": "test", "alert_type": "simple", "lhs_exchange": "NSE", "lhs_tradingsymbol": "INFY", "lhs_attribute": "LastTradedPrice", "operator": ">", "rhs_type": "constant", "rhs_constant": value}, true)
		require.Error(t, err)
	}
}
