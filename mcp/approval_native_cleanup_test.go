package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/storage"
	"github.com/zerodha/kite-mcp-server/kc"
)

func nativeCleanupRequest(owner string) *sdk.CallToolRequest {
	return &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": {owner}}}}
}

func nativeCleanupContext(store *tradeApprovals, manager *kc.Manager) context.Context {
	ctx := context.WithValue(context.Background(), approvalContextKey{}, store)
	return context.WithValue(ctx, approvalManagerKey{}, manager)
}

func TestNativeLogoutReclaimsPersistedApprovals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	store := &tradeApprovals{state: state, pending: storage.NewMemory[string, pendingTradeApproval]()}
	ctx := nativeCleanupContext(store, f.manager)
	request := nativeCleanupRequest("approval-test")
	execute := func() (*sdk.CallToolResult, error) {
		t.Fatal("logout must not submit an order")
		return nil, nil
	}
	first, err := approvalFromContext(ctx, request, "orders_place", nil, execute)
	require.NoError(t, err)
	require.NotEmpty(t, first.RequestState)

	// A different active grant must retain its pending approval.
	_, _, err = f.manager.SessionManager().GetOrCreate("other-grant")
	require.NoError(t, err)
	require.NoError(t, f.manager.SessionManager().UpdateCredentials("other-grant", &kc.KiteCredentials{UserID: "other", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)}))
	other, err := approvalFromContext(ctx, nativeCleanupRequest("other-grant"), "orders_place", nil, execute)
	require.NoError(t, err)
	require.NotEmpty(t, other.RequestState)

	request.Params.Arguments = json.RawMessage(`{"mode":"logout"}`)
	result, err := (&SessionTool{}).Handler(f.manager)(ctx, request)
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, 1, store.pending.Len())
	_, exists := store.pending.Lookup(first.RequestState)
	require.False(t, exists)
	stored, err := state.Load("native_approvals")
	require.NoError(t, err)
	require.NotContains(t, stored, first.RequestState)
	require.Contains(t, stored, other.RequestState)

	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, state = sqliteApprovalFixture(t, path)
	store = &tradeApprovals{state: state, pending: storage.NewMemory[string, pendingTradeApproval]()}
	require.NoError(t, store.restoreState())
	_, exists = store.pending.Lookup(first.RequestState)
	require.False(t, exists, "logout cleanup must survive restart")
	require.Equal(t, 1, store.pending.Len())
}

func TestNativeOrphansAreReclaimedBeforeAdmissionAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	deadline := time.Now().Add(tradeApprovalTimeout)
	changes := make([]storage.Change, 0, maxPendingTradeApprovals)
	for i := 0; i < maxPendingTradeApprovals; i++ {
		change, err := storage.Encode("native_approvals", fmt.Sprintf("orphan-%d", i), storedNativeApproval{Owner: "ended-grant", Operation: "orders_place", Snapshot: []byte("null"), Expires: deadline})
		require.NoError(t, err)
		changes = append(changes, change)
	}
	require.NoError(t, state.Apply(changes...))
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, state = sqliteApprovalFixture(t, path)
	store := &tradeApprovals{state: state, pending: storage.NewMemory[string, pendingTradeApproval]()}
	require.NoError(t, store.restoreState())
	require.Equal(t, maxPendingTradeApprovals, store.pending.Len())
	result, err := approvalFromContext(nativeCleanupContext(store, f.manager), nativeCleanupRequest("approval-test"), "orders_place", nil, func() (*sdk.CallToolResult, error) {
		t.Fatal("admission must not submit an order")
		return nil, nil
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "orphan approvals must not exhaust the global pool")
	require.NotEmpty(t, result.RequestState)
	require.Equal(t, 1, store.pending.Len())
	stored, err := state.Load("native_approvals")
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Contains(t, stored, result.RequestState)
}

func TestNativeContinuationRejectsEndedGrant(t *testing.T) {
	for _, scenario := range []string{"active", "logged-out", "expired", "unauthenticated", "transport"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			f, state := sqliteApprovalFixture(t, path)
			store := &tradeApprovals{state: state, pending: storage.NewMemory[string, pendingTradeApproval]()}
			ctx := nativeCleanupContext(store, f.manager)
			request := nativeCleanupRequest("approval-test")
			if scenario == "expired" {
				_, _, err := f.manager.SessionManager().GetOrCreate("expired-grant")
				require.NoError(t, err)
				require.NoError(t, f.manager.SessionManager().UpdateCredentials("expired-grant", &kc.KiteCredentials{UserID: "user", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)}))
				request = nativeCleanupRequest("expired-grant")
			}
			if scenario == "transport" {
				request.Extra = nil
			}
			calls := 0
			execute := func() (*sdk.CallToolResult, error) { calls++; return NewToolResultText("executed"), nil }
			first, err := approvalFromContext(ctx, request, "orders_place", nil, execute)
			require.NoError(t, err)
			require.NotEmpty(t, first.RequestState)
			switch scenario {
			case "logged-out":
				_, err = f.manager.SessionManager().Terminate("approval-test")
				require.NoError(t, err)
			case "unauthenticated":
				require.NoError(t, f.manager.SessionManager().UpdateCredentials("approval-test", nil))
			case "expired":
				// Persist an expired grant and reload it, leaving the approval's
				// own five-minute deadline in the future.
				sessions, err := state.Load("sessions")
				require.NoError(t, err)
				var session map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(sessions["expired-grant"], &session))
				session["ExpiresAt"], err = json.Marshal(time.Now().Add(-time.Second))
				require.NoError(t, err)
				encoded, err := json.Marshal(session)
				require.NoError(t, err)
				require.NoError(t, state.Apply(storage.Change{Bucket: "sessions", Key: "expired-grant", Value: encoded}))
				f.manager.SessionManager().StopCleanupRoutine()
				require.NoError(t, state.Close())
				f, state = sqliteApprovalFixture(t, path)
				store = &tradeApprovals{state: state, pending: storage.NewMemory[string, pendingTradeApproval]()}
				require.NoError(t, store.restoreState())
				ctx = nativeCleanupContext(store, f.manager)
				request = nativeCleanupRequest("expired-grant")
			}
			request.Params.RequestState = first.RequestState
			request.Params.InputResponses = sdk.InputResponseMap{"trade_approval": &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}}
			result, err := approvalFromContext(ctx, request, "orders_place", nil, execute)
			require.NoError(t, err)
			allowed := scenario == "active" || scenario == "transport"
			require.Equal(t, !allowed, result.IsError)
			if allowed {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
			}
			require.Zero(t, store.pending.Len())
			stored, err := state.Load("native_approvals")
			require.NoError(t, err)
			require.Empty(t, stored)
		})
	}
}

func TestOrdersNativeAdmissionReclaimsPersistedOrphans(t *testing.T) {
	f, state := sqliteApprovalFixture(t, filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, storage.Put(state, "native_approvals", "orphan", storedNativeApproval{Owner: "ended-grant", Operation: "orders_place", Snapshot: []byte("null"), Expires: time.Now().Add(tradeApprovalTimeout)}))
	server, err := NewServerWithStorage(&sdk.Implementation{Name: "test", Version: "1"}, state)
	require.NoError(t, err)
	f.b.Attach(server)
	tool := &OrdersTool{}
	server.AddTool(tool.Definition(), sdk.ToolHandler(tool.Handler(f.manager)))
	endpoint := httptest.NewServer(f.b.oauth.Middleware(NewHTTPHandler(server, nil)))
	t.Cleanup(endpoint.Close)
	clientID, err := f.b.oauth.RegisterClient([]string{"https://client.example/callback"}, "Native test")
	require.NoError(t, err)
	token := approvalBearer(t, f, clientID)
	elicited := 0
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, &sdk.ClientOptions{
		ElicitationHandler: func(_ context.Context, _ *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
			elicited++
			stored, err := state.Load("native_approvals")
			require.NoError(t, err)
			require.NotContains(t, stored, "orphan", "real order handler must validate owning grants before admission")
			return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
		},
	})
	cs, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: browserBearerTransport{token}}}, &sdk.ClientSessionOptions{ProtocolVersion: "2026-07-28"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	result := callBrowserOrder(t, cs, browserOrderArgs())
	require.False(t, result.IsError)
	require.Equal(t, 1, elicited)
	require.EqualValues(t, 1, f.calls.Load())
}

type nativeCleanupFailureState struct{ storage.State }

func (s nativeCleanupFailureState) Apply(changes ...storage.Change) error {
	for _, change := range changes {
		if change.Bucket == "native_approvals" && change.Value == nil {
			return errors.New("injected native cleanup failure")
		}
	}
	return s.State.Apply(changes...)
}

func TestNativeCleanupFailureIsReportedOnLogoutAndAdmission(t *testing.T) {
	f, state := sqliteApprovalFixture(t, filepath.Join(t.TempDir(), "state.db"))
	pending := pendingTradeApproval{owner: "approval-test", operation: "orders_place", expires: time.Now().Add(tradeApprovalTimeout)}
	require.NoError(t, storage.Put(state, "native_approvals", "pending", storedNativeApproval{Owner: pending.owner, Operation: pending.operation, Expires: pending.expires}))
	store := &tradeApprovals{state: nativeCleanupFailureState{state}, pending: storage.NewMemory[string, pendingTradeApproval]()}
	store.pending.Set("pending", pending)
	ctx := nativeCleanupContext(store, f.manager)
	request := nativeCleanupRequest("approval-test")
	request.Params.Arguments = json.RawMessage(`{"mode":"logout"}`)
	result, err := (&SessionTool{}).Handler(f.manager)(ctx, request)
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "native approval cleanup")
	require.Equal(t, 1, store.pending.Len(), "failed persistence must retain in-memory state for retry")
	stored, err := state.Load("native_approvals")
	require.NoError(t, err)
	require.Contains(t, stored, "pending")
	_, err = approvalFromContext(ctx, request, "orders_place", nil, func() (*sdk.CallToolResult, error) {
		t.Fatal("cleanup failure must not submit an order")
		return nil, nil
	})
	require.ErrorContains(t, err, "injected native cleanup failure")
	// A retry can finish the already-logged-out session's cleanup.
	store.state = state
	result, err = (&SessionTool{}).Handler(f.manager)(ctx, request)
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Zero(t, store.pending.Len())
}
