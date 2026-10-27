package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestApprovalModernDiscoveryAndWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := NewServer(&sdk.Implementation{Name: "server", Version: "1"})
	executed, elicited := 0, 0
	capabilityOverride := ""
	responseAction := "accept"
	modernCalls := 0
	server.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if method == "tools/list" || method == "tools/call" {
				if call, ok := req.(*sdk.CallToolRequest); ok && call.ProtocolVersion() == "2026-07-28" {
					modernCalls++
				}
				meta := req.GetParams().GetMeta()
				switch capabilityOverride {
				case "omit":
					delete(meta, sdk.MetaKeyClientCapabilities)
				case "url":
					meta[sdk.MetaKeyClientCapabilities] = map[string]any{"elicitation": map[string]any{"url": map[string]any{}}}
				}
			}
			return next(ctx, method, req)
		}
	})
	orders := &OrdersTool{}
	server.AddTool(orders.Definition(), func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return withTradeApproval(ctx, req, "orders_place", GetArguments(req), func() (*sdk.CallToolResult, error) {
			executed++
			return NewToolResultText("executed"), nil
		})
	})
	var sessions []*sdk.ClientSession
	for _, capable := range []bool{false, true, false} {
		opts := &sdk.ClientOptions{}
		if capable {
			opts.ElicitationHandler = func(_ context.Context, req *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
				elicited++
				require.Equal(t, "form", req.Params.Mode)
				require.Contains(t, req.Params.Message, "IDEA")
				if responseAction == "decline" {
					capabilityOverride = "omit"
				}
				return &sdk.ElicitResult{Action: responseAction, Content: map[string]any{"confirm": true}}, nil
			}
		}
		client := sdk.NewClient(&sdk.Implementation{Name: "client", Version: "1"}, opts)
		ct, st := sdk.NewInMemoryTransports()
		ss, err := server.Connect(ctx, st, nil)
		require.NoError(t, err)
		defer func() { require.NoError(t, ss.Close()) }()
		cs, err := client.Connect(ctx, ct, &sdk.ClientSessionOptions{ProtocolVersion: "2026-07-28"})
		require.NoError(t, err)
		defer func() { require.NoError(t, cs.Close()) }()
		sessions = append(sessions, cs)
	}
	// Interleave clients to detect shared tool-definition mutation.
	for round := 0; round < 2; round++ {
		for _, cs := range sessions {
			listing, err := cs.ListTools(ctx, nil)
			require.NoError(t, err)
			require.Len(t, listing.Tools, 1)
			schema, err := json.Marshal(listing.Tools[0].InputSchema)
			require.NoError(t, err)
			require.Contains(t, string(schema), `"place"`)
			require.Contains(t, string(schema), `"modify"`)
			require.Contains(t, string(schema), `"cancel"`)
		}
	}
	for _, cs := range sessions {
		result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "orders", Arguments: map[string]any{"mode": "place", "tradingsymbol": "IDEA"}})
		require.NoError(t, err)
		require.False(t, result.IsError)
	}
	require.Equal(t, 1, elicited)
	require.Equal(t, 3, executed)
	require.Positive(t, modernCalls, "must exercise 2026 discovery and request metadata")
	// Omitting capabilities on an approval retry cannot bypass a decline.
	responseAction = "decline"
	declined, err := sessions[1].CallTool(ctx, &sdk.CallToolParams{Name: "orders", Arguments: map[string]any{"mode": "place", "tradingsymbol": "IDEA"}})
	require.NoError(t, err)
	require.True(t, declined.IsError)
	require.Equal(t, 3, executed)

	// A previously capable session cannot supply capabilities for a later request.
	for _, override := range []string{"omit", "url"} {
		capabilityOverride = override
		listing, err := sessions[1].ListTools(ctx, nil)
		require.NoError(t, err)
		schema, err := json.Marshal(listing.Tools[0].InputSchema)
		require.NoError(t, err)
		require.Contains(t, string(schema), `"place"`)
		result, err := sessions[1].CallTool(ctx, &sdk.CallToolParams{Name: "orders", Arguments: map[string]any{"mode": "place"}})
		require.NoError(t, err)
		require.False(t, result.IsError)
	}
	require.Equal(t, 2, elicited)
	require.Equal(t, 5, executed)
}

func TestApprovalRoundTripStateIntegrity(t *testing.T) {
	for _, scenario := range []string{"accept", "decline", "false", "missing", "forged", "different owner", "changed", "expired", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			store := &tradeApprovals{pending: make(map[string]pendingTradeApproval)}
			ctx := context.WithValue(context.Background(), approvalContextKey{}, store)
			req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"owner"}}}}
			details := map[string]any{"quantity": 2}
			executed := 0
			execute := func() (*sdk.CallToolResult, error) { executed++; return NewToolResultText("executed"), nil }
			first, err := approvalFromContext(ctx, req, "orders_place", details, execute)
			require.NoError(t, err)
			require.NotEmpty(t, first.InputRequests)
			require.Zero(t, executed)
			req.Params.RequestState = first.RequestState
			response := &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}
			req.Params.InputResponses = sdk.InputResponseMap{"trade_approval": response}
			switch scenario {
			case "decline":
				response.Action = "decline"
			case "false":
				response.Content["confirm"] = false
			case "missing":
				req.Params.InputResponses = nil
			case "forged":
				req.Params.RequestState = "forged"
			case "different owner":
				req.Extra.Header.Set("X-Kite-Session-Id", "other")
			case "changed":
				details["quantity"] = 3
			case "expired":
				pending := store.pending[first.RequestState]
				pending.expires = time.Now().Add(-time.Second)
				store.pending[first.RequestState] = pending
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := approvalFromContext(ctx, req, "orders_place", details, execute)
			require.NoError(t, err)
			require.Equal(t, scenario != "accept", result.IsError)
			if scenario == "accept" {
				require.Equal(t, 1, executed)
				result, err = approvalFromContext(ctx, req, "orders_place", details, execute)
				require.NoError(t, err)
				require.True(t, result.IsError)
				require.Equal(t, 1, executed)
			} else {
				require.Zero(t, executed)
			}
		})
	}
}

func TestApprovalCapacityIsolatesSessionsAndReclaimsSlots(t *testing.T) {
	store := &tradeApprovals{pending: make(map[string]pendingTradeApproval)}
	ctx := context.WithValue(context.Background(), approvalContextKey{}, store)
	execute := func() (*sdk.CallToolResult, error) {
		t.Fatal("capacity checks must not execute transactions")
		return nil, nil
	}
	request := func(owner string) *sdk.CallToolRequest {
		return &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{owner}}}}
	}
	create := func(owner string) *sdk.CallToolResult {
		t.Helper()
		result, err := approvalFromContext(ctx, request(owner), "orders_place", nil, execute)
		require.NoError(t, err)
		return result
	}
	var first *sdk.CallToolResult
	for i := 0; i < maxPendingTradeApprovalsPerSession; i++ {
		result := create("session-a")
		require.False(t, result.IsError)
		if first == nil {
			first = result
		}
	}
	require.True(t, create("session-a").IsError)
	require.False(t, create("session-b").IsError, "one saturated session must not block another")
	require.Len(t, store.pending, maxPendingTradeApprovalsPerSession+1)

	// Consuming an approval frees capacity for its owner, including declines.
	req := request("session-a")
	req.Params.RequestState = first.RequestState
	req.Params.InputResponses = sdk.InputResponseMap{"trade_approval": &sdk.ElicitResult{Action: "decline"}}
	declined, err := approvalFromContext(ctx, req, "orders_place", nil, execute)
	require.NoError(t, err)
	require.True(t, declined.IsError)
	require.False(t, create("session-a").IsError)
	require.True(t, create("session-a").IsError)

	// Expired state counts against neither the session nor the global limit.
	for token, pending := range store.pending {
		if pending.owner == "session-a" {
			pending.expires = time.Now().Add(-time.Second)
			store.pending[token] = pending
			break
		}
	}
	require.False(t, create("session-a").IsError)
	require.Len(t, store.pending, maxPendingTradeApprovalsPerSession+1)

	// Keep the global bound as a backstop across distinct sessions.
	for len(store.pending) < maxPendingTradeApprovals {
		token := fmt.Sprintf("existing-%d", len(store.pending))
		store.pending[token] = pendingTradeApproval{owner: token, expires: time.Now().Add(time.Minute)}
	}
	require.True(t, create("session-c").IsError)
	require.Len(t, store.pending, maxPendingTradeApprovals)
}
