package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/kc"
)

func orderMutationSession(t *testing.T, manager *kc.Manager, options *sdk.ClientOptions) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	server := NewServer(&sdk.Implementation{Name: "test-server", Version: "1"})
	tool := &OrdersTool{}
	handler := tool.Handler(manager)
	server.AddTool(tool.Definition(), func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		req.Extra = &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"approval-test"}}}
		return handler(ctx, req)
	})
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "1"}, options)
	ct, st := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ss.Close()) })
	cs, err := client.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cs.Close()) })
	return cs
}

func TestOrderMutationRejectsInvalidIntegers(t *testing.T) {
	var requests atomic.Int32
	manager := approvalTestManager(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected Kite request", http.StatusInternalServerError)
	})
	cs := orderMutationSession(t, manager, nil)
	for _, mode := range []string{"place", "modify"} {
		for _, field := range []string{"quantity", "validity_ttl", "disclosed_quantity", "iceberg_legs", "iceberg_quantity"} {
			for _, invalid := range []struct {
				name  string
				value any
			}{
				{"fraction", 1.9}, {"string", "100"}, {"null", nil}, {"negative", -1}, {"overflow", float64(1 << 63)}, {"boolean", true},
			} {
				t.Run(mode+"/"+field+"/"+invalid.name, func(t *testing.T) {
					args := map[string]any{"mode": mode, "order_id": "order-1", "exchange": "NSE", "tradingsymbol": "INFY", "transaction_type": "BUY", "quantity": 3, "product": "CNC", "order_type": "LIMIT", "price": 1500}
					args[field] = invalid.value
					result, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "orders", Arguments: args})
					require.NoError(t, err)
					require.True(t, result.IsError)
					require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "parameter '"+field+"'")
					require.Zero(t, requests.Load(), "invalid arguments must be rejected before accessing Kite")
				})
			}
		}
	}
}

func TestOrderIntegerBoundsAndTTL(t *testing.T) {
	for _, field := range []string{"quantity", "validity_ttl", "iceberg_legs", "iceberg_quantity"} {
		require.Error(t, validateOrderIntegers(map[string]any{field: 0}), field)
		require.NoError(t, validateOrderIntegers(map[string]any{field: 1}), field)
		require.NoError(t, validateOrderIntegers(map[string]any{field: float64(2147483647)}), field)
		require.Error(t, validateOrderIntegers(map[string]any{field: float64(2147483648)}), field)
	}
	require.NoError(t, validateOrderIntegers(map[string]any{"disclosed_quantity": 0}))
	require.NoError(t, validateOrderIntegers(map[string]any{}), "omitted modification fields stay unchanged")
	require.Error(t, validateOrderIntegers(map[string]any{"validity": "TTL"}))
	require.NoError(t, validateOrderIntegers(map[string]any{"validity": "TTL", "validity_ttl": 15}))
}

func TestOrderModifyTTLApprovalAndRequest(t *testing.T) {
	var mutations, elicitations atomic.Int32
	manager := approvalTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, err := w.Write([]byte(`{"status":"success","data":[{"order_id":"order-1","variety":"regular","order_type":"LIMIT","quantity":3,"validity":"DAY"}]}`))
			require.NoError(t, err)
			return
		}
		mutations.Add(1)
		require.EqualValues(t, 1, elicitations.Load())
		require.Equal(t, http.MethodPut, r.Method)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "TTL", r.Form.Get("validity"))
		require.Equal(t, "15", r.Form.Get("validity_ttl"))
		require.Empty(t, r.Form.Get("quantity"), "unspecified quantity must stay unchanged")
		_, err := w.Write([]byte(`{"status":"success","data":{"order_id":"order-1"}}`))
		require.NoError(t, err)
	})
	cs := orderMutationSession(t, manager, &sdk.ClientOptions{ElicitationHandler: func(_ context.Context, req *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
		elicitations.Add(1)
		require.Zero(t, mutations.Load())
		require.Contains(t, req.Params.Message, `"validity_ttl": "15"`)
		return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
	}})
	result, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "orders", Arguments: map[string]any{"mode": "modify", "order_id": "order-1", "order_type": "LIMIT", "validity": "TTL", "validity_ttl": 15}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.EqualValues(t, 1, mutations.Load())
}

func TestGTTMutationHonorsCancellation(t *testing.T) {
	for _, mode := range []string{"place", "modify", "delete"} {
		for _, when := range []string{"before request", "during request"} {
			t.Run(mode+"/"+when, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reached := make(chan struct{})
				release := make(chan struct{})
				var requests atomic.Int32
				manager := approvalTestManager(t, func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					close(reached)
					select {
					case <-r.Context().Done():
					case <-release:
					}
				})
				defer close(release)
				args, err := json.Marshal(map[string]any{"mode": mode, "trigger_id": 123, "exchange": "NSE", "tradingsymbol": "INFY", "last_price": 1500, "transaction_type": "BUY", "product": "CNC", "trigger_type": "single", "trigger_value": 1400, "quantity": 1, "limit_price": 1400})
				require.NoError(t, err)
				req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: args}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"approval-test"}}}}
				if when == "before request" {
					cancel()
				}
				done := make(chan *sdk.CallToolResult, 1)
				go func() {
					result, err := (&GTTTool{}).Handler(manager)(ctx, req)
					if err != nil {
						done <- nil
						return
					}
					done <- result
				}()
				if when == "during request" {
					select {
					case <-reached:
						cancel()
					case <-time.After(3 * time.Second):
						t.Fatal("GTT request did not reach mock Kite API")
					}
				}
				select {
				case result := <-done:
					require.NotNil(t, result)
					require.True(t, result.IsError)
				case <-time.After(3 * time.Second):
					t.Fatal("GTT handler ignored cancellation")
				}
				if when == "before request" {
					require.Zero(t, requests.Load())
				} else {
					require.EqualValues(t, 1, requests.Load())
				}
			})
		}
	}
}
