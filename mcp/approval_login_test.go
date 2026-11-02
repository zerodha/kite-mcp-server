package mcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/oauth"
)

func reviewCallback(t *testing.T, b *BrowserApprovals, target, account string) *http.Request {
	t.Helper()
	response := browserPage(t, b, target, nil)
	require.Equal(t, http.StatusSeeOther, response.Code)
	login, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	query, err := url.ParseQuery(login.Query().Get("redirect_params"))
	require.NoError(t, err)
	query.Set("request_token", account)
	req := httptest.NewRequest("GET", "https://mcp.example/callback?"+query.Encode(), nil)
	req.RemoteAddr = fmt.Sprintf("[2001:db8:%x:%x::1]:1234", []byte(account)[:1], []byte(query.Get("state"))[:2])
	return req
}

func TestReviewLoginRejectsForgedExpiredAndReplayedCallbacks(t *testing.T) {
	for _, scenario := range []string{"wrong-account", "wrong-state", "missing-token", "duplicate-token", "expired", "replay", "logout", "concurrent-credentials"} {
		t.Run(scenario, func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
			id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
			req := reviewCallback(t, f.b, id, "user")
			q := req.URL.Query()
			flowID, err := f.manager.SessionSigner().VerifySessionID(q.Get("session_id"))
			require.NoError(t, err)
			pending, err := f.manager.SessionManager().Get(flowID)
			require.NoError(t, err)
			flow := pending.OAuthData.(*reviewLogin)
			switch scenario {
			case "wrong-account":
				q.Set("request_token", "other")
			case "wrong-state":
				q.Set("state", strings.Repeat("f", 64))
			case "missing-token":
				q.Del("request_token")
			case "duplicate-token":
				q.Add("request_token", "other")
			case "expired":
				flow.expires = time.Now().Add(-time.Second)
			case "logout":
				_, err = f.manager.SessionManager().Terminate("approval-test")
				require.NoError(t, err)
			case "concurrent-credentials":
				require.NoError(t, f.manager.SessionManager().UpdateCredentials("approval-test", flow.previous))
			}
			req.URL.RawQuery = q.Encode()
			handlers := oauth.NewHandlers(f.b.oauth, f.manager, f.manager.Logger)
			w := httptest.NewRecorder()
			handlers.HandleCallback(w, req)
			require.Empty(t, w.Result().Cookies())
			require.Zero(t, f.calls.Load(), "authentication alone must never mutate orders")
			if scenario == "replay" {
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "action=\""+f.b.approvalURL(id)+"\"")
				proof := pageCSRF(t, w)
				// The form works from a browser without any cookie jar.
				require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"approve"}}, "").Code)
				require.EqualValues(t, 1, f.calls.Load())
				w = httptest.NewRecorder()
				handlers.HandleCallback(w, req)
			}
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), `name="csrf"`)
			require.False(t, flow.verified && scenario != "replay")
		})
	}
}

func TestReviewRequiresFreshLoginForEveryOrderAndPermissionChange(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	review := browserPage(t, f.b, id, identity)
	proof := pageCSRF(t, review)
	other := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	require.Equal(t, http.StatusForbidden, postBrowserDecision(f.b, other, nil, url.Values{"csrf": {proof}, "decision": {"approve"}}, "").Code)
	require.Equal(t, http.StatusForbidden, postBrowserDecision(f.b, "manage/approval-test", nil, url.Values{"csrf": {proof}, "decision": {"revoke"}}, "").Code)
	require.Equal(t, http.StatusSeeOther, browserPage(t, f.b, other, nil).Code)
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, nil, url.Values{"csrf": {proof}, "decision": {"approve"}, "remember": {"yes"}}, "").Code)
	require.EqualValues(t, 1, f.calls.Load())
	// A successful review leaves no browser session. Revocation needs its own login.
	require.Equal(t, http.StatusSeeOther, browserPage(t, f.b, "manage/approval-test", nil).Code)
	manage := browserPage(t, f.b, "manage/approval-test", identity)
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, "manage/approval-test", nil, url.Values{"csrf": {pageCSRF(t, manage)}, "decision": {"revoke"}}, "").Code)
	require.Contains(t, callBrowserOrder(t, cs, map[string]any{"mode": "permissions"}).Content[0].(*sdk.TextContent).Text, `"future_orders_allowed":false`)
}
