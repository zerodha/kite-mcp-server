package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func validateStructuredOutput(t *testing.T, tool *sdk.Tool, result *sdk.CallToolResult) map[string]any {
	t.Helper()
	require.False(t, result.IsError)
	require.NotNil(t, tool)
	require.NotNil(t, tool.OutputSchema, tool.Name)
	require.NotNil(t, result.StructuredContent, tool.Name)
	raw, err := json.Marshal(tool.OutputSchema)
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(raw, &schema))
	require.Equal(t, "object", schema.Type)
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	raw, err = json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var structured map[string]any
	require.NoError(t, json.Unmarshal(raw, &structured))
	require.NotNil(t, structured)
	require.NoError(t, resolved.Validate(structured), "%s: %s", tool.Name, raw)
	return structured
}

// Exercise discovery, handler registration and serialization through the SDK,
// including clients that do not advertise Apps support.
func TestStructuredOutputsOnWire(t *testing.T) {
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		data, ok := map[string]string{
			"/user/profile":               `{"user_id":"test-user","user_name":"Test User"}`,
			"/user/margins":               `{"equity":{},"commodity":{}}`,
			"/portfolio/holdings/compact": `[{"tradingsymbol":"INFY","exchange":"NSE","quantity":3}]`,
			"/portfolio/holdings":         `[{"tradingsymbol":"INFY","exchange":"NSE","quantity":3}]`,
			"/portfolio/positions":        `{"day":[],"net":[]}`,
			"/orders":                     `[]`,
			"/orders/order-1":             `[]`,
			"/orders/order-1/trades":      `null`,
			"/trades":                     `[]`,
			"/gtt/triggers":               `[]`,
			"/alerts":                     `[]`,
			"/alerts/alert-1":             `{"uuid":"alert-1"}`,
			"/alerts/alert-1/history":     `[]`,
			"/mf/holdings":                `[]`,
		}[r.URL.Path]
		if !ok {
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := io.WriteString(w, `{"status":"success","data":`+data+`}`)
		require.NoError(t, err)
	})
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			server := NewServer(&sdk.Implementation{Name: "structured-test", Version: "1"})
			RegisterTools(server, manager, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
			server.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
				return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
					if call, ok := req.(*sdk.CallToolRequest); ok {
						call.Extra = &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"read-contract"}}}
					}
					return next(ctx, method, req)
				}
			})
			ct, st := sdk.NewInMemoryTransports()
			ss, err := server.Connect(ctx, st, nil)
			require.NoError(t, err)
			defer func() { require.NoError(t, ss.Close()) }()
			client := sdk.NewClient(&sdk.Implementation{Name: "client", Version: "1"}, nil)
			cs, err := client.Connect(ctx, ct, &sdk.ClientSessionOptions{ProtocolVersion: version})
			require.NoError(t, err)
			defer func() { require.NoError(t, cs.Close()) }()
			listed, err := cs.ListTools(ctx, nil)
			require.NoError(t, err)
			tools := make(map[string]*sdk.Tool)
			for _, tool := range listed.Tools {
				require.NotNil(t, tool.OutputSchema, tool.Name)
				tools[tool.Name] = tool
			}
			for _, tc := range []struct {
				name string
				args map[string]any
			}{
				{"portfolio", map[string]any{"mode": "profile"}},
				{"portfolio", map[string]any{"mode": "margins"}},
				{"portfolio", map[string]any{"mode": "holdings"}},
				{"portfolio", map[string]any{"mode": "holdings", "type": "compact"}},
				{"portfolio", map[string]any{"mode": "positions"}},
				{"orders", map[string]any{"mode": "list"}},
				{"orders", map[string]any{"mode": "history", "order_id": "order-1"}},
				{"orders", map[string]any{"mode": "trades", "order_id": "order-1"}},
				{"orders", map[string]any{"mode": "all_trades"}},
				{"gtt", map[string]any{"mode": "list"}},
				{"alerts", map[string]any{"mode": "get"}},
				{"alerts", map[string]any{"mode": "get", "uuids": []string{"alert-1"}, "history": true}},
				{"mutual_funds", map[string]any{"mode": "holdings"}},
				{"session", map[string]any{"mode": "status"}},
				{"help", map[string]any{"key": "/orders"}},
			} {
				t.Run(tc.name+"/"+SafeAssertString(tc.args["mode"], ""), func(t *testing.T) {
					result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tc.name, Arguments: tc.args})
					require.NoError(t, err)
					structured := validateStructuredOutput(t, tools[tc.name], result)
					text := result.Content[0].(*sdk.TextContent).Text
					if tc.name == "help" {
						require.Equal(t, "/orders", structured["key"])
						require.Equal(t, text, structured["text"])
						return
					}
					var legacy any
					require.NoError(t, json.Unmarshal([]byte(text), &legacy))
					if _, ok := legacy.(map[string]any); ok {
						require.Equal(t, legacy, structured)
					} else {
						require.Equal(t, legacy, structured["data"])
						require.Len(t, structured, 1)
					}
				})
			}
			bad, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "orders", Arguments: map[string]any{"mode": "history"}})
			require.NoError(t, err)
			require.True(t, bad.IsError)
			require.Nil(t, bad.StructuredContent)
		})
	}
}

func TestMarshalResponsePreservesJSON(t *testing.T) {
	manager := readContractManager(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected upstream call") })
	handler := NewToolHandler(manager)
	for _, value := range []any{
		map[string]any{"large_id": int64(9007199254740993), "nested": map[string]any{"zero": 0}},
		[]int64{9007199254740993}, []any{}, ([]any)(nil), nil, "message", true,
	} {
		result, err := handler.MarshalResponse(value, "test")
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := result.Content[0].(*sdk.TextContent).Text
		structured, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		if len(text) > 0 && text[0] == '{' {
			require.Equal(t, text, string(structured))
		} else {
			require.Equal(t, `{"data":`+text+`}`, string(structured))
		}
	}
	result, err := handler.MarshalResponse(math.NaN(), "test")
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Nil(t, result.StructuredContent)
}

func TestStructuredMutationOutputs(t *testing.T) {
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		responses := map[string]string{
			"POST /gtt/triggers":       `{"trigger_id":123}`,
			"PUT /gtt/triggers/123":    `{"trigger_id":123}`,
			"DELETE /gtt/triggers/123": `{"trigger_id":123}`,
			"POST /alerts":             `{"uuid":"alert-1"}`,
			"PUT /alerts/alert-1":      `{"uuid":"alert-1"}`,
			"DELETE /alerts":           `null`,
		}
		data, ok := responses[r.Method+" "+r.URL.Path]
		if !ok {
			t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := io.WriteString(w, `{"status":"success","data":`+data+`}`)
		require.NoError(t, err)
	})
	for _, mode := range []string{"place", "modify", "delete"} {
		t.Run("gtt/"+mode, func(t *testing.T) {
			tool := &GTTTool{}
			result := callReadContract(t, tool, manager, map[string]any{
				"mode": mode, "trigger_id": 123, "exchange": "NSE", "tradingsymbol": "INFY",
				"last_price": 1500, "transaction_type": "BUY", "product": "CNC",
				"trigger_type": "single", "trigger_value": 1400, "quantity": 1, "limit_price": 1400,
			})
			structured := validateStructuredOutput(t, tool.Definition(), result)
			require.Equal(t, float64(123), structured["trigger_id"])
		})
	}
	for _, mode := range []string{"create", "modify", "delete"} {
		t.Run("alerts/"+mode, func(t *testing.T) {
			tool := &AlertsTool{}
			result := callReadContract(t, tool, manager, map[string]any{
				"mode": mode, "uuids": []string{"alert-1"}, "name": "Test alert", "alert_type": "simple",
				"lhs_exchange": "NSE", "lhs_tradingsymbol": "INFY", "lhs_attribute": "LastTradedPrice",
				"operator": ">=", "rhs_type": "constant", "rhs_constant": 1500,
			})
			validateStructuredOutput(t, tool.Definition(), result)
		})
	}
	tool := &SessionTool{}
	for range 2 {
		result := callReadContract(t, tool, manager, map[string]any{"mode": "logout"})
		validateStructuredOutput(t, tool.Definition(), result)
	}
}
