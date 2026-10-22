package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

func TestTradeApprovalResponses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *sdk.ElicitResult
		err      error
		allowed  bool
	}{
		{"accept", &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil, true},
		{"decline", &sdk.ElicitResult{Action: "decline", Content: map[string]any{"confirm": true}}, nil, false},
		{"cancel", &sdk.ElicitResult{Action: "cancel"}, nil, false},
		{"false", &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": false}}, nil, false},
		{"missing confirmation", &sdk.ElicitResult{Action: "accept"}, nil, false},
		{"string confirmation", &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": "true"}}, nil, false},
		{"missing response", nil, nil, false},
		{"transport error", nil, errors.New("connection lost"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			result, err := elicitTradeApproval(context.Background(), time.Second, func(ctx context.Context, p *sdk.ElicitParams) (*sdk.ElicitResult, error) {
				require.Equal(t, "form", p.Mode)
				require.Contains(t, p.Message, "BUY")
				return tc.response, tc.err
			}, "orders_place", map[string]any{"transaction_type": "BUY"}, func() (*sdk.CallToolResult, error) { calls++; return NewToolResultText("executed"), nil })
			require.NoError(t, err)
			require.Equal(t, !tc.allowed, result.IsError)
			if tc.allowed {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
				require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "Trade approval failed")
			}
		})
	}
}

func TestTradeApprovalDeadlineAndIntegrity(t *testing.T) {
	for _, scenario := range []string{"timeout", "late accept", "cancelled", "parameters changed"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			details := map[string]any{"quantity": 1}
			calls := 0
			result, err := elicitTradeApproval(ctx, time.Millisecond, func(ctx context.Context, _ *sdk.ElicitParams) (*sdk.ElicitResult, error) {
				switch scenario {
				case "timeout":
					<-ctx.Done()
					return nil, ctx.Err()
				case "late accept":
					<-ctx.Done()
				case "cancelled":
					cancel()
				case "parameters changed":
					details["quantity"] = 2
				}
				return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
			}, "orders_place", details, func() (*sdk.CallToolResult, error) { calls++; return NewToolResultText("executed"), nil })
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Zero(t, calls)
		})
	}
}

// Exercise actual tools/call -> elicitation/create -> Kite HTTP request, rather
// than testing an approval boolean supplied as a tool argument.
func TestOrdersElicitationWire(t *testing.T) {
	for _, mode := range []string{"place", "modify", "cancel"} {
		for _, decision := range []string{"accept", "decline", "cancel", "unsupported", "url only", "api failure"} {
			t.Run(mode+"/"+decision, func(t *testing.T) {
				var mutations, elicitations atomic.Int32
				var approved atomic.Bool
				manager := approvalTestManager(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.Method == http.MethodGet {
						_, _ = w.Write([]byte(`{"status":"success","data":[{"order_id":"order-1","exchange":"NSE","tradingsymbol":"INFY","transaction_type":"BUY","quantity":3,"product":"CNC","order_type":"LIMIT","price":1500,"validity":"DAY"}]}`))
						return
					}
					require.True(t, approved.Load(), "Kite mutation must follow explicit approval")
					mutations.Add(1)
					require.NoError(t, r.ParseForm())
					if mode != "cancel" {
						require.Equal(t, "3", r.Form.Get("quantity"))
						require.Equal(t, "1500", r.Form.Get("price"))
						require.Equal(t, "LIMIT", r.Form.Get("order_type"))
						require.Empty(t, r.Form.Get("approved"))
					}
					if decision == "api failure" {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"status":"error","error_type":"InputException","message":"exchange rejected"}`))
						return
					}
					_, _ = w.Write([]byte(`{"status":"success","data":{"order_id":"order-1"}}`))
				})
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				server := NewServer(&sdk.Implementation{Name: "test-server", Version: "1"})
				tool := &OrdersTool{}
				handler := tool.Handler(manager)
				server.AddTool(tool.Definition(), func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
					req.Extra = &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"approval-test"}}}
					return handler(ctx, req)
				})
				options := &sdk.ClientOptions{}
				if decision != "unsupported" {
					options.ElicitationHandler = func(_ context.Context, req *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
						attempt := elicitations.Add(1)
						if attempt > 1 {
							return &sdk.ElicitResult{Action: "decline"}, nil
						}
						require.Zero(t, mutations.Load())
						for _, detail := range []string{"INFY", "NSE", "BUY", "3", "CNC", "LIMIT", "1500", "DAY"} {
							require.Contains(t, req.Params.Message, detail)
						}
						action := decision
						if action == "api failure" {
							action = "accept"
						}
						approved.Store(action == "accept")
						// A client cannot substitute changed order parameters in its response.
						return &sdk.ElicitResult{Action: action, Content: map[string]any{"confirm": true}}, nil
					}
				}
				if decision == "url only" {
					options.Capabilities = &sdk.ClientCapabilities{Elicitation: &sdk.ElicitationCapabilities{URL: &sdk.URLElicitationCapabilities{}}}
				}
				client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "1"}, options)
				ct, st := sdk.NewInMemoryTransports()
				ss, err := server.Connect(ctx, st, nil)
				require.NoError(t, err)
				defer func() { require.NoError(t, ss.Close()) }()
				cs, err := client.Connect(ctx, ct, nil)
				require.NoError(t, err)
				defer func() { require.NoError(t, cs.Close()) }()
				args := map[string]any{"mode": mode, "exchange": "NSE", "tradingsymbol": "INFY", "transaction_type": "BUY", "quantity": 3, "product": "CNC", "order_type": "LIMIT", "price": 1500, "validity": "DAY", "order_id": "order-1", "approved": true}
				result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "orders", Arguments: args})
				require.NoError(t, err)
				allowed := decision == "accept" || decision == "api failure"
				if allowed {
					require.EqualValues(t, 1, mutations.Load())
					require.EqualValues(t, 1, elicitations.Load())
				} else {
					require.Zero(t, mutations.Load())
					require.True(t, result.IsError)
				}
				if decision == "accept" {
					require.False(t, result.IsError)
					// Replaying the same tool arguments cannot reuse an earlier acceptance.
					again, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "orders", Arguments: args})
					require.NoError(t, err)
					require.True(t, again.IsError)
					require.EqualValues(t, 2, elicitations.Load())
					require.EqualValues(t, 1, mutations.Load())
				}
				if decision == "api failure" {
					require.True(t, result.IsError)
					message := result.Content[0].(*sdk.TextContent).Text
					require.Contains(t, message, "exchange rejected")
					require.False(t, strings.Contains(message, "Trade approval failed"))
				}
				if decision == "unsupported" || decision == "url only" {
					require.Zero(t, elicitations.Load())
					require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "unsupported_client")
				}
			})
		}
	}
}

func TestOrderApprovalUsesKiteOmitRulesAndChecksLiveParams(t *testing.T) {
	params := kiteconnect.OrderParams{OrderType: "LIMIT", Price: 1500}
	details := map[string]any{"changes": approvalOrderParams{&params}}
	called := false
	result, err := elicitTradeApproval(context.Background(), time.Second, func(_ context.Context, p *sdk.ElicitParams) (*sdk.ElicitResult, error) {
		require.Contains(t, p.Message, `"order_type": "LIMIT"`)
		require.Contains(t, p.Message, `"price": "1500"`)
		require.NotContains(t, p.Message, `"quantity"`, "omitted quantity must not appear as a change to zero")
		params.Price = 1600
		return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
	}, "orders_modify", details, func() (*sdk.CallToolResult, error) { called = true; return NewToolResultText("executed"), nil })
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.False(t, called)
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "parameters_changed")
}

type cancellingApprovalDetails struct {
	calls  int
	cancel context.CancelFunc
}

func (d *cancellingApprovalDetails) MarshalJSON() ([]byte, error) {
	d.calls++
	if d.calls == 2 {
		d.cancel()
	}
	return []byte(`{"quantity":1}`), nil
}

func TestTradeApprovalCancellationDuringVerification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executed := false
	result, err := elicitTradeApproval(ctx, time.Second, func(context.Context, *sdk.ElicitParams) (*sdk.ElicitResult, error) {
		return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
	}, "orders_place", &cancellingApprovalDetails{cancel: cancel}, func() (*sdk.CallToolResult, error) {
		executed = true
		return NewToolResultText("executed"), nil
	})
	require.NoError(t, err)
	require.False(t, executed)
	require.True(t, result.IsError)
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "cancelled")
}

func TestTradeApprovalDowngradedInitialize(t *testing.T) {
	for _, version := range []string{"2026-07-28", "2025-11-25", "2025-06-18"} {
		t.Run(version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server := NewServer(&sdk.Implementation{Name: "server", Version: "1"})
			executed, elicited := 0, 0
			server.AddTool(&sdk.Tool{Name: "approval", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				return withTradeApproval(ctx, req, "orders_place", map[string]any{"quantity": 1}, func() (*sdk.CallToolResult, error) { executed++; return NewToolResultText("executed"), nil })
			})
			client := sdk.NewClient(&sdk.Implementation{Name: "client", Version: "1"}, &sdk.ClientOptions{
				ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
					elicited++
					return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
				},
			})
			// Exercise a legacy initialize handshake requesting the newer version,
			// rather than letting this SDK's discover path pick an older version first.
			client.AddSendingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
				return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
					if method == "initialize" {
						req.GetParams().(*sdk.InitializeParams).ProtocolVersion = version
					}
					return next(ctx, method, req)
				}
			})
			ct, st := sdk.NewInMemoryTransports()
			ss, err := server.Connect(ctx, st, nil)
			require.NoError(t, err)
			defer func() { require.NoError(t, ss.Close()) }()
			cs, err := client.Connect(ctx, ct, &sdk.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
			require.NoError(t, err)
			defer func() { require.NoError(t, cs.Close()) }()
			want := version
			if version == "2026-07-28" {
				want = "2025-11-25"
			}
			require.Equal(t, want, cs.InitializeResult().ProtocolVersion)
			result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "approval"})
			require.NoError(t, err)
			require.False(t, result.IsError, "%s", result.Content[0].(*sdk.TextContent).Text)
			require.Equal(t, 1, executed)
			require.Equal(t, 1, elicited)
		})
	}
}

func approvalTestManager(t *testing.T, upstream http.HandlerFunc) *kc.Manager {
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
	_, _, err = manager.SessionManager().GetOrCreate("approval-test")
	require.NoError(t, err)
	require.NoError(t, manager.SessionManager().UpdateCredentials("approval-test", &kc.KiteCredentials{UserID: "user", AccessToken: "test-access", ExpiresAt: time.Now().Add(time.Hour)}))
	return manager
}
