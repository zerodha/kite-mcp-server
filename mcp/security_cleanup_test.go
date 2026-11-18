package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/storage"
	"github.com/zerodha/kite-mcp-server/kc"
)

func TestSubmittedRevocationCannotRevokeFreshPermission(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	initial := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	initialProof := pageCSRF(t, browserPage(t, f.b, initial, identity))
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, initial, nil, url.Values{"csrf": {initialProof}, "decision": {"approve"}, "remember": {"yes"}}, "").Code)
	manage := "manage/approval-test"
	proof := pageCSRF(t, browserPage(t, f.b, manage, identity))
	form := url.Values{"csrf": {proof}, "decision": {"revoke"}}
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, manage, nil, form, "").Code)
	_, err := f.manager.SessionManager().Get(strings.SplitN(proof, ".", 2)[0])
	require.Error(t, err, "accepted form must release its persisted login slot")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	approve := pageCSRF(t, browserPage(t, f.b, id, identity))
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {approve}, "decision": {"approve"}, "remember": {"yes"}}, "").Code)
	require.True(t, f.b.permissions.Get("approval-test").allowed)
	require.Equal(t, http.StatusForbidden, postBrowserDecision(f.b, manage, nil, form, "").Code)
	require.True(t, f.b.permissions.Get("approval-test").allowed, "old proof revoked fresh permission")
	require.EqualValues(t, 2, f.calls.Load())
}

func TestLogoutReclaimsSavedApprovalsAndReviewsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	cs, identity := f.connect(t, "cleanup-grant", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	proof := pageCSRF(t, browserPage(t, f.b, id, identity))
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"save"}}, "").Code)
	// Start and abandon a separate review, then end the owning grant.
	pageCSRF(t, browserPage(t, f.b, "manage/cleanup-grant", identity))
	ctx := context.WithValue(context.Background(), browserApprovalsKey{}, f.b)
	result, err := (&SessionTool{}).Handler(f.manager)(ctx, &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: json.RawMessage(`{"mode":"logout"}`)}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": {"cleanup-grant"}}}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Nil(t, f.b.pending.Get(id))
	stored, err := state.Load("browser_orders")
	require.NoError(t, err)
	require.NotContains(t, stored, id)
	sessions, err := state.Load("sessions")
	require.NoError(t, err)
	for _, raw := range sessions {
		require.NotContains(t, string(raw), "cleanup-grant", "orphan login persisted after logout")
	}
	require.Zero(t, f.calls.Load())
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, _ = sqliteApprovalFixture(t, path)
	require.Nil(t, f.b.pending.Get(id))
	_, err = f.manager.SessionExpiry("cleanup-grant")
	require.Error(t, err)
}

func TestOrphanSavedApprovalsAreReclaimedAtAdmissionAndRestore(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprintf("restart=%v", restart), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			f, state := sqliteApprovalFixture(t, path)
			for i := 0; i < maxPendingTradeApprovals; i++ {
				p := &browserOrder{id: randomApprovalSecret(), session: "logged-out", user: "user", status: "approved_for_later", expires: time.Now().Add(7 * 24 * time.Hour), retainUntil: time.Now().Add(7*24*time.Hour + 15*time.Minute)}
				f.b.pending.Set(p.id, p)
				require.NoError(t, f.b.persistLocked([]*browserOrder{p}))
			}
			if restart {
				f.manager.SessionManager().StopCleanupRoutine()
				require.NoError(t, state.Close())
				f, state = sqliteApprovalFixture(t, path)
				require.Zero(t, f.b.pending.Len())
			}
			cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
			browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
			require.Equal(t, 1, f.b.pending.Len())
			stored, err := state.Load("browser_orders")
			require.NoError(t, err)
			require.Len(t, stored, 1)
		})
	}
}

func TestAccountCannotMultiplySavedCapacityAcrossGrants(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
	for i := 0; i < maxPendingBrowserApprovalsPerAccount; i++ {
		owner := fmt.Sprintf("grant-%d", i/maxPendingTradeApprovalsPerSession)
		_, _, err := f.manager.SessionManager().GetOrCreate(owner)
		require.NoError(t, err)
		require.NoError(t, f.manager.SessionManager().UpdateCredentials(owner, &kc.KiteCredentials{UserID: "user", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)}))
		p := &browserOrder{id: randomApprovalSecret(), session: owner, user: "user", status: "approved_for_later", expires: time.Now().Add(time.Hour), retainUntil: time.Now().Add(time.Hour)}
		f.b.pending.Set(p.id, p)
	}
	result := callBrowserOrder(t, cs, browserOrderArgs())
	require.True(t, result.IsError)
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "busy")
}

func TestOrphanCleanupPreservesSubmissionOutcomes(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	for _, status := range []string{"submitting", "submitted", "failed"} {
		p := &browserOrder{id: randomApprovalSecret(), session: "ended-grant", user: "user", status: status, expires: time.Now().Add(time.Hour), retainUntil: time.Now().Add(time.Hour)}
		f.b.pending.Set(p.id, p)
	}
	require.NoError(t, f.b.cleanupLocked(time.Now()))
	require.Equal(t, 3, f.b.pending.Len(), "orphan cleanup discarded an in-flight or uncertain submission")
	require.Zero(t, f.calls.Load())
}

func TestUpgradeInvalidatesLegacyReusableReviewProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	proof := pageCSRF(t, browserPage(t, f.b, id, identity))
	flowID := strings.SplitN(proof, ".", 2)[0]
	sessions, err := state.Load("sessions")
	require.NoError(t, err)
	var session map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(sessions[flowID], &session))
	var review map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(session["Data"], &review))
	delete(review, "Version")
	session["Data"], err = json.Marshal(review)
	require.NoError(t, err)
	encoded, err := json.Marshal(session)
	require.NoError(t, err)
	require.NoError(t, state.Apply(storage.Change{Bucket: "sessions", Key: flowID, Value: encoded}))
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, state = sqliteApprovalFixture(t, path)
	_, err = f.manager.SessionManager().Get(flowID)
	require.Error(t, err, "legacy review state not reclaimed during upgrade")
	require.Equal(t, http.StatusForbidden, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"approve"}}, "").Code)
	require.Zero(t, f.calls.Load())
	freshProof := pageCSRF(t, browserPage(t, f.b, id, identity))
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {freshProof}, "decision": {"approve"}}, "").Code)
	require.EqualValues(t, 1, f.calls.Load())
}
