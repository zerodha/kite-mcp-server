package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/storage"
	"github.com/zerodha/kite-mcp-server/oauth"
)

func sqliteApprovalFixture(t *testing.T, path string) (*browserApprovalFixture, *storage.SQLite) {
	t.Helper()
	state, err := storage.OpenSQLite(path, bytes.Repeat([]byte{4}, 32))
	require.NoError(t, err)
	t.Cleanup(func() { _ = state.Close() })
	return newBrowserApprovalFixtureWithState(t, state), state
}

func approvalBearer(t *testing.T, f *browserApprovalFixture, clientID string) string {
	t.Helper()
	verifier := strings.Repeat("v", 43)
	hash := sha256.Sum256([]byte(verifier))
	code, err := f.b.oauth.CreateAuthCode(clientID, "https://client.example/callback", "user", "approval-test", base64.RawURLEncoding.EncodeToString(hash[:]))
	require.NoError(t, err)
	token, err := f.b.oauth.ExchangeCode(&oauth.TokenRequest{Code: code, ClientID: clientID, RedirectURI: "https://client.example/callback", CodeVerifier: verifier})
	require.NoError(t, err)
	return token
}

func restoredApprovalClient(t *testing.T, f *browserApprovalFixture, token string) *sdk.ClientSession {
	t.Helper()
	server, err := NewServerWithStorage(&sdk.Implementation{Name: "test", Version: "1"}, f.manager.StateStore())
	require.NoError(t, err)
	f.b.Attach(server)
	orders := &OrdersTool{}
	server.AddTool(orders.Definition(), sdk.ToolHandler(orders.Handler(f.manager)))
	endpoint := httptest.NewServer(f.b.oauth.Middleware(NewHTTPHandler(server, nil)))
	t.Cleanup(endpoint.Close)
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: browserBearerTransport{token}}}, &sdk.ClientSessionOptions{ProtocolVersion: "2026-07-28"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestBrowserReviewSurvivesRestartsBeforeCallbackAndBeforeSubmit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	callback := reviewCallback(t, f.b, id, "user")
	// Restart with the user away at Kite login.
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, state = sqliteApprovalFixture(t, path)
	w := httptest.NewRecorder()
	oauth.NewHandlers(f.b.oauth, f.manager, f.manager.Logger).HandleCallback(w, callback)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	proof := pageCSRF(t, w)
	// Restart again after login succeeded but before the review form was posted.
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, state = sqliteApprovalFixture(t, path)
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"approve"}}, "").Code)
	require.EqualValues(t, 1, f.calls.Load())
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, _ = sqliteApprovalFixture(t, path)
	require.Equal(t, "submitted", f.b.pending.Get(id).status)
	require.NotNil(t, f.b.pending.Get(id).result)
	require.Zero(t, f.b.permissions.Len(), "ordinary approval must not create an empty permission record")
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"approve"}}, "").Code)
	require.Zero(t, f.calls.Load(), "replayed form after restart must not submit")
	replay := httptest.NewRecorder()
	oauth.NewHandlers(f.b.oauth, f.manager, f.manager.Logger).HandleCallback(replay, callback)
	require.Equal(t, http.StatusForbidden, replay.Code)
}

func TestSavedApprovalAndConsumptionSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	review := browserPage(t, f.b, id, identity)
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {pageCSRF(t, review)}, "decision": {"save"}}, "").Code)
	token := approvalBearer(t, f, f.b.pending.Get(id).client)
	deadline := f.b.pending.Get(id).expires
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, state = sqliteApprovalFixture(t, path)
	require.Equal(t, deadline.UnixNano(), f.b.pending.Get(id).expires.UnixNano())
	cs = restoredApprovalClient(t, f, token)
	require.False(t, callBrowserOrder(t, cs, browserOrderArgs()).IsError)
	require.EqualValues(t, 1, f.calls.Load())
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, _ = sqliteApprovalFixture(t, path)
	cs = restoredApprovalClient(t, f, token)
	require.Equal(t, "approval_required", approvalObservation(t, callBrowserOrder(t, cs, browserOrderArgs()))["status"])
	require.Zero(t, f.calls.Load(), "consumed saved approval must never become available after restart")
}

func TestBroadPermissionAndRevocationSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	review := browserPage(t, f.b, id, identity)
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {pageCSRF(t, review)}, "decision": {"approve"}, "remember": {"yes"}}, "").Code)
	token := approvalBearer(t, f, f.b.pending.Get(id).client)
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, state = sqliteApprovalFixture(t, path)
	cs = restoredApprovalClient(t, f, token)
	require.False(t, callBrowserOrder(t, cs, browserOrderArgs()).IsError)
	require.EqualValues(t, 1, f.calls.Load())
	review = browserPage(t, f.b, "manage/approval-test", identity)
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, "manage/approval-test", nil, url.Values{"csrf": {pageCSRF(t, review)}, "decision": {"revoke"}}, "").Code)
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, _ = sqliteApprovalFixture(t, path)
	cs = restoredApprovalClient(t, f, token)
	require.Equal(t, "approval_required", approvalObservation(t, callBrowserOrder(t, cs, browserOrderArgs()))["status"])
	require.Zero(t, f.calls.Load())
}

func TestInterruptedSubmissionRemainsConsumed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	f.b.mu.Lock()
	p := f.b.pending.Get(id)
	p.status = "submitting"
	require.NoError(t, f.b.persistLocked([]*browserOrder{p}))
	f.b.mu.Unlock()
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, _ = sqliteApprovalFixture(t, path)
	require.Equal(t, "failed", f.b.pending.Get(id).status)
	require.True(t, f.b.pending.Get(id).result.IsError)
	require.Zero(t, f.calls.Load())
}

func TestStorageFailureBeforeApprovalClaimPreventsSubmission(t *testing.T) {
	f, state := sqliteApprovalFixture(t, filepath.Join(t.TempDir(), "state.db"))
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	review := browserPage(t, f.b, id, identity)
	require.NoError(t, state.Close())
	response := postBrowserDecision(f.b, id, nil, url.Values{"csrf": {pageCSRF(t, review)}, "decision": {"approve"}}, "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Zero(t, f.calls.Load())
}

func TestNativeApprovalConsumptionSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	open := func() (*tradeApprovals, *storage.SQLite) {
		state, err := storage.OpenSQLite(path, bytes.Repeat([]byte{2}, 32))
		require.NoError(t, err)
		t.Cleanup(func() { _ = state.Close() })
		store := &tradeApprovals{state: state, pending: storage.NewMemory[string, pendingTradeApproval]()}
		require.NoError(t, store.restoreState())
		return store, state
	}
	store, state := open()
	ctx := context.WithValue(context.Background(), approvalContextKey{}, store)
	req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": {"grant"}}}}
	calls := 0
	execute := func() (*sdk.CallToolResult, error) {
		calls++
		return NewToolResultError("submission outcome uncertain"), nil
	}
	details := map[string]any{"quantity": 1}
	first, err := approvalFromContext(ctx, req, "orders_place", details, execute)
	require.NoError(t, err)
	require.NotEmpty(t, first.RequestState)
	require.NoError(t, state.Close())
	store, state = open()
	ctx = context.WithValue(context.Background(), approvalContextKey{}, store)
	req.Params.RequestState = first.RequestState
	req.Params.InputResponses = sdk.InputResponseMap{"trade_approval": &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}}
	result, err := approvalFromContext(ctx, req, "orders_place", details, execute)
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Equal(t, 1, calls)
	require.NoError(t, state.Close())
	store, _ = open()
	ctx = context.WithValue(context.Background(), approvalContextKey{}, store)
	result, err = approvalFromContext(ctx, req, "orders_place", details, execute)
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Equal(t, 1, calls)
}
