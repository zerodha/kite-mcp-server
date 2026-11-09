package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBrowserApprovalSaveExplanationBelongsToSaveForm(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	page := browserPage(t, f.b, id, identity)
	forms := regexp.MustCompile(`(?s)<form\b[^>]*>.*?</form>`).FindAllString(page.Body.String(), -1)
	require.Len(t, forms, 2)
	require.Contains(t, forms[0], "Place this order")
	require.NotContains(t, forms[0], "No order will be placed now")
	require.Contains(t, forms[1], "No order will be placed now")
	require.Contains(t, forms[1], "Changed order details will need a new approval")
	require.Contains(t, forms[1], `aria-describedby="save-order-scope approval-deadline"`)
	require.Contains(t, forms[1], "Approve for later. Do not place now")
	require.Less(t, strings.Index(forms[1], "No order will be placed now"), strings.Index(forms[1], "Approve for later. Do not place now"))
}

func TestBrowserApprovalSaveKeepsExistingBroadPermissionVisible(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}, "")
	require.EqualValues(t, 1, f.calls.Load())
	args := browserOrderArgs()
	args["approval_method"] = "browser"
	id = browserApprovalID(t, callBrowserOrder(t, cs, args))
	page := browserPage(t, f.b, id, identity)
	require.Contains(t, page.Body.String(), "Permission for other orders is already enabled")
	require.NotContains(t, page.Body.String(), "Changed order details will need a new approval")
	csrf = pageCSRF(t, page)
	outcome := postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"save"}}, "")
	require.Equal(t, http.StatusOK, outcome.Code)
	require.EqualValues(t, 1, f.calls.Load(), "saving must not submit another order")
	require.Contains(t, outcome.Body.String(), "Approval saved. Order not placed")
	require.Contains(t, outcome.Body.String(), "Pi can place, modify, and cancel orders without asking")
	require.Contains(t, outcome.Body.String(), "Saving this approval has not turned off that permission")
	require.Contains(t, outcome.Body.String(), "Ask me before every order")
	require.Contains(t, outcome.Body.String(), "Manage saved approvals")
	status := approvalObservation(t, callBrowserOrder(t, cs, map[string]any{"mode": "approval_status", "approval_id": id}))
	require.Equal(t, true, status["future_orders_allowed"])
	// The page must disclose the existing permission because it still applies
	// to requests with different details; saving does not silently revoke it.
	changed := browserOrderArgs()
	changed["quantity"] = 2
	result := callBrowserOrder(t, cs, changed)
	require.False(t, result.IsError)
	require.EqualValues(t, 2, f.calls.Load())
}

func TestBrowserApprovalSaveExpiryConcurrentGET(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	// A save extends the mutable expiry while another browser opens the same
	// approval. Run with -race to cover the GET handler's expiry snapshot.
	for attempt := 0; attempt < 30; attempt++ {
		p := &browserOrder{id: randomApprovalSecret(), session: "approval-test", user: "user", clientName: "Pi", operation: "orders_place", status: "approval_required", expires: time.Now().Add(time.Minute), retainUntil: time.Now().Add(time.Hour), snapshot: []byte(`{"parameters":{"quantity":"1"}}`)}
		f.b.mu.Lock()
		f.b.pending.Set(p.id, p)
		f.b.mu.Unlock()
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				r := httptest.NewRequest(http.MethodGet, f.b.approvalURL(p.id), nil)
				f.b.serveHTTP(httptest.NewRecorder(), r)
			}()
		}
		outcome := httptest.NewRecorder()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := httptest.NewRequest(http.MethodPost, f.b.approvalURL(p.id), strings.NewReader("decision=save"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			f.b.decide(outcome, r, p)
		}()
		close(start)
		wg.Wait()
		require.Equal(t, http.StatusOK, outcome.Code)
		require.Equal(t, "approved_for_later", p.status)
	}
	require.Zero(t, f.calls.Load())
}

func TestBrowserApprovalSaveDisclosesItsOwnDeadline(t *testing.T) {
	for _, broad := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact-order-only", true: "existing-broad-permission"}[broad], func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
			sessionExpiry, err := f.manager.SessionExpiry("approval-test")
			require.NoError(t, err)
			args := browserOrderArgs()
			if broad {
				id := browserApprovalID(t, callBrowserOrder(t, cs, args))
				csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
				postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}, "")
				// Distinct deadlines must not cause the broad permission's date to
				// replace the exact-order deadline before or after saving.
				f.b.mu.Lock()
				permission := f.b.permissions.Get("approval-test")
				permission.expires = sessionExpiry.Add(-time.Hour)
				f.b.permissions.Set("approval-test", permission)
				f.b.mu.Unlock()
				args["approval_method"] = "browser"
			}
			id := browserApprovalID(t, callBrowserOrder(t, cs, args))
			f.b.mu.Lock()
			reviewExpiry := f.b.pending.Get(id).expires
			f.b.mu.Unlock()
			require.Greater(t, sessionExpiry.Sub(reviewExpiry), time.Hour)
			page := browserPage(t, f.b, id, identity)
			saveForm := regexp.MustCompile(`(?s)<form class="save-order-form".*?</form>`).FindString(page.Body.String())
			savedDeadline := sessionExpiry.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST")
			require.Contains(t, saveForm, "If saved, this approval expires at "+savedDeadline)
			require.Less(t, strings.Index(saveForm, savedDeadline), strings.Index(saveForm, "Approve for later. Do not place now"))
			require.Contains(t, page.Body.String(), "This review expires at "+reviewExpiry.In(time.FixedZone("IST", 19800)).Format("15:04 IST"))
			calls := f.calls.Load()
			outcome := postBrowserDecision(f.b, id, identity, url.Values{"csrf": {pageCSRF(t, page)}, "decision": {"save"}}, "")
			require.Equal(t, http.StatusOK, outcome.Code)
			require.Equal(t, calls, f.calls.Load(), "saving must not send an order")
			require.Contains(t, outcome.Body.String(), "Saved approval expires at "+savedDeadline)
			status := approvalObservation(t, callBrowserOrder(t, cs, map[string]any{"mode": "approval_status", "approval_id": id}))
			actualExpiry, err := time.Parse(time.RFC3339, status["expires_at"].(string))
			require.NoError(t, err)
			require.Equal(t, savedDeadline, actualExpiry.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST"))
		})
	}
}
