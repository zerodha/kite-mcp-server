package mcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/oauth"
)

func openReviewFromPeer(b *BrowserApprovals, target, peer string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, b.approvalURL(target), nil)
	r.RemoteAddr = peer
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}

func TestLinkHolderCannotFillOwnersReviewQuotaFromOnePrefix(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	for i := 0; i < 8; i++ {
		w := openReviewFromPeer(f.b, "manage/approval-test", fmt.Sprintf("[2001:db8:bad::%x]:1", i+1))
		require.Equal(t, http.StatusSeeOther, w.Code)
	}
	pageCSRF(t, browserPage(t, f.b, id, identity))
	require.Zero(t, f.calls.Load())
}

func TestAnonymousLinkOpensPreserveVerifiedProofAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	for i := 0; i < 8; i++ {
		peer := fmt.Sprintf("[2001:db8:%x::1]:1", i/4)
		require.Equal(t, http.StatusSeeOther, openReviewFromPeer(f.b, "manage/approval-test", peer).Code)
	}
	// Anonymous starts do not consume authenticated review admission.
	proof := pageCSRF(t, browserPage(t, f.b, id, identity))
	flowID := strings.SplitN(proof, ".", 2)[0]
	f.manager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	f, _ = sqliteApprovalFixture(t, path)
	for i := 0; i < 16; i++ {
		peer := fmt.Sprintf("[2001:db8:%x::1]:1", i+16)
		require.Equal(t, http.StatusSeeOther, openReviewFromPeer(f.b, "manage/approval-test", peer).Code)
		_, err := f.manager.SessionManager().Get(flowID)
		require.NoError(t, err, "an authenticated review was evicted")
	}
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"approve"}}, "").Code)
	require.EqualValues(t, 1, f.calls.Load())
}

func TestSavedCapacityCannotBlockNewReviewsOrImmediateSubmission(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprintf("restart=%v", restart), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			f, state := sqliteApprovalFixture(t, path)
			var saved []*browserOrder
			now := time.Now()
			for i := 0; i < maxSavedTradeApprovals; i++ {
				owner := fmt.Sprintf("saved-grant-%d", i/8)
				user := fmt.Sprintf("saved-user-%d", i/32)
				if i%8 == 0 {
					_, _, err := f.manager.SessionManager().GetOrCreate(owner)
					require.NoError(t, err)
					require.NoError(t, f.manager.SessionManager().UpdateCredentials(owner, &kc.KiteCredentials{UserID: user, AccessToken: "access", ExpiresAt: now.Add(time.Hour)}))
				}
				p := &browserOrder{id: randomApprovalSecret(), session: owner, user: user, status: "approved_for_later", expires: now.Add(7 * 24 * time.Hour), retainUntil: now.Add(7*24*time.Hour + 15*time.Minute)}
				f.b.pending.Set(p.id, p)
				saved = append(saved, p)
			}
			require.NoError(t, f.b.persistLocked(saved))
			if restart {
				f.manager.SessionManager().StopCleanupRoutine()
				require.NoError(t, state.Close())
				f, state = sqliteApprovalFixture(t, path)
			}
			cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
			id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
			require.Equal(t, maxSavedTradeApprovals+1, f.b.pending.Len())
			proof := pageCSRF(t, browserPage(t, f.b, id, identity))
			require.Equal(t, http.StatusTooManyRequests, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"save"}}, "").Code)
			require.Equal(t, "approval_required", f.b.pending.Get(id).status)
			require.Zero(t, f.calls.Load())
			proof = pageCSRF(t, browserPage(t, f.b, id, identity))
			require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"approve"}}, "").Code)
			require.EqualValues(t, 1, f.calls.Load())
			stored, err := state.Load("browser_orders")
			require.NoError(t, err)
			require.Len(t, stored, maxSavedTradeApprovals+1)
		})
	}
}

func TestReviewCallbackSurvivesConcurrentAnonymousOpens(t *testing.T) {
	for attempt := 0; attempt < 12; attempt++ {
		f := newBrowserApprovalFixture(t)
		cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
		id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
		callback := reviewCallback(t, f.b, id, "user")
		for i := 0; i < 7; i++ {
			require.Equal(t, http.StatusSeeOther, openReviewFromPeer(f.b, "manage/approval-test", fmt.Sprintf("[2001:db8:%x::1]:1", i/4+4)).Code)
		}
		response := httptest.NewRecorder()
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			oauth.NewHandlers(f.b.oauth, f.manager, f.manager.Logger).HandleCallback(response, callback)
		}()
		go func() {
			defer wg.Done()
			<-start
			openReviewFromPeer(f.b, "manage/approval-test", "[2001:db8:ffff::1]:1")
		}()
		close(start)
		wg.Wait()
		require.Equal(t, http.StatusOK, response.Code)
		proof := pageCSRF(t, response)
		flowID := strings.SplitN(proof, ".", 2)[0]
		_, err := f.manager.SessionManager().Get(flowID)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"approve"}}, "").Code)
		require.EqualValues(t, 1, f.calls.Load())
	}
}

func TestAbandonedTabsCannotLockOutOwnerSharingSamePeer(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	for i := 0; i < 8; i++ {
		require.Equal(t, http.StatusSeeOther, openReviewFromPeer(f.b, "manage/approval-test", "192.0.2.1:1234").Code)
	}
	pageCSRF(t, browserPage(t, f.b, id, identity))
	require.Zero(t, f.calls.Load())
}

func TestOwnerSignInSurvivesInterleavedLinkFloodWithoutAllocations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	f, state := sqliteApprovalFixture(t, path)
	cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	owner := openReviewFromPeer(f.b, id, "[2001:db8:0:1::1]:1")
	require.Equal(t, http.StatusSeeOther, owner.Code)
	login, err := url.Parse(owner.Header().Get("Location"))
	require.NoError(t, err)
	q, err := url.ParseQuery(login.Query().Get("redirect_params"))
	require.NoError(t, err)
	before, err := state.Load("sessions")
	require.NoError(t, err)
	for i := 0; i < 2048; i++ {
		peer := fmt.Sprintf("[2001:db8:bad:%x::1]:1", i)
		require.Equal(t, http.StatusSeeOther, openReviewFromPeer(f.b, "manage/approval-test", peer).Code)
	}
	after, err := state.Load("sessions")
	require.NoError(t, err)
	require.Len(t, after, len(before), "anonymous opens allocated persisted sessions")
	q.Set("request_token", "user")
	callback := httptest.NewRequest(http.MethodGet, "https://mcp.example/callback?"+q.Encode(), nil)
	callback.RemoteAddr = "[2001:db8:0:1::1]:1"
	response := httptest.NewRecorder()
	oauth.NewHandlers(f.b.oauth, f.manager, f.manager.Logger).HandleCallback(response, callback)
	proof := pageCSRF(t, response)
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"approve"}}, "").Code)
	require.EqualValues(t, 1, f.loginCalls.Load())
	require.EqualValues(t, 1, f.calls.Load())
}

func TestConcurrentSealedCallbackExchangesOnlyOnce(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	callback := reviewCallback(t, f.b, id, "user")
	responses := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range responses {
		responses[i] = httptest.NewRecorder()
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			oauth.NewHandlers(f.b.oauth, f.manager, f.manager.Logger).HandleCallback(responses[i], callback.Clone(callback.Context()))
		}(i)
	}
	close(start)
	wg.Wait()
	success := 0
	for _, response := range responses {
		if response.Code == http.StatusOK {
			success++
		} else {
			require.Equal(t, http.StatusForbidden, response.Code)
		}
	}
	require.Equal(t, 1, success)
	require.EqualValues(t, 1, f.loginCalls.Load(), "replayed callback exchanged credentials twice")
	require.Zero(t, f.calls.Load())
}
