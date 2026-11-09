package mcp

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zerodha/kite-mcp-server/internal/audit"
	"github.com/zerodha/kite-mcp-server/kc"
)

// reviewLogin is temporary server-held state for one exact review. Neither
// the MCP bearer token nor possession of its URL can complete Kite sign-in.
// The form secret is revealed only after a matching-account token exchange.
type reviewLogin struct {
	mu                                       sync.Mutex
	broker                                   *BrowserApprovals
	id, target, session, user, state, secret string
	previous                                 *kc.KiteCredentials
	expires                                  time.Time
	consumed, verified                       bool
}

func (b *BrowserApprovals) startReviewLogin(w http.ResponseWriter, r *http.Request, target, session, user string, expires time.Time) {
	operation := b.reviewMetricOperation(target)
	event := "login_failed"
	defer func() { b.metricEvent(operation, event) }()
	if !b.loginLimit.Allow(b.oauth.ClientIP(r)) {
		b.render(w, http.StatusTooManyRequests, browserOrderPage{Title: "Too many sign-in attempts", Message: "Wait a minute, then open the approval link again."})
		return
	}
	if !time.Now().Before(expires) {
		b.render(w, http.StatusGone, browserOrderPage{Title: "Approval expired", Message: "Ask your app to check the request status before starting another approval."})
		return
	}
	current, err := b.manager.SessionManager().Get(session)
	if err != nil || current.Credentials == nil || current.Credentials.UserID != user {
		b.render(w, http.StatusForbidden, browserOrderPage{Title: "Connection unavailable", Message: "Reconnect Kite in your app, then request a new approval."})
		return
	}
	flow := &reviewLogin{broker: b, target: target, session: session, user: user, state: randomApprovalSecret(), secret: randomApprovalSecret(), previous: current.Credentials, expires: expires}
	id, err := b.manager.SessionManager().GeneratePendingAuthorization(flow)
	if err != nil {
		b.render(w, http.StatusTooManyRequests, browserOrderPage{Title: "Too many sign-in attempts", Message: "Wait a minute, then open the approval link again."})
		return
	}
	flow.id = id
	loginURL, err := b.manager.GenerateLoginURL(id, flow.state)
	if err != nil {
		_, _ = b.manager.SessionManager().Terminate(id)
		b.render(w, http.StatusServiceUnavailable, browserOrderPage{Title: "Sign-in unavailable", Message: "Wait a few minutes, then open the approval link again."})
		return
	}
	event = "login_started"
	http.Redirect(w, r, loginURL, http.StatusSeeOther)
}

// HandleKiteLoginCallback is dispatched by the ordinary signed Kite callback.
// Login never submits an order: the returned form still needs explicit review.
func (f *reviewLogin) HandleKiteLoginCallback(w http.ResponseWriter, r *http.Request) {
	operation := f.broker.reviewMetricOperation(f.target)
	event := "login_failed"
	defer func() { f.broker.metricEvent(operation, event) }()
	approvalHTTPHeaders(w)
	q := r.URL.Query()
	f.mu.Lock()
	valid := !f.consumed && time.Now().Before(f.expires) && len(q["session_id"]) == 1 && len(q["state"]) == 1 && len(q["request_token"]) == 1 && q.Get("request_token") != "" && len(q.Get("request_token")) <= 4096 && subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(f.state)) == 1
	if valid {
		f.consumed = true
		if err := f.broker.manager.SessionManager().PersistSessionData(f.id); err != nil {
			f.mu.Unlock()
			http.Error(w, "Storage unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	f.mu.Unlock()
	if !valid {
		f.broker.render(w, http.StatusForbidden, browserOrderPage{Title: "Sign-in link unavailable", Message: "This link has expired or was already used. Ask your app to check the request status before starting another approval."})
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), f.expires)
	defer cancel()
	fresh, err := f.broker.manager.CompleteLoginContext(ctx, q.Get("request_token"))
	if err != nil || fresh.UserID != f.user {
		reason := "account_mismatch"
		if err != nil {
			reason = "token_exchange_failed"
		}
		audit.Record(f.broker.manager.Logger, "order_review_login_failed", audit.Actor{SessionID: f.session}, "expected_user_id", f.user, "approval_ref", audit.Ref(f.target), "reason", reason, "source_ip", f.broker.oauth.ClientIP(r))
		f.broker.render(w, http.StatusForbidden, browserOrderPage{Title: "Sign-in could not be verified", Account: f.user, Message: "Sign in to the Kite account shown above to review this request. No request was sent to Kite.", RetryURL: f.broker.approvalURL(f.target)})
		return
	}
	if ctx.Err() != nil || !time.Now().Before(f.expires) || f.broker.manager.SessionManager().ReplaceApprovalCredentials(f.session, f.previous, fresh) != nil {
		f.broker.render(w, http.StatusForbidden, browserOrderPage{Title: "Review unavailable", Message: "The login changed or this approval expired. No request was sent to Kite. Ask your app to check the request status."})
		return
	}
	f.mu.Lock()
	f.verified = true
	if err := f.broker.manager.SessionManager().PersistSessionData(f.id); err != nil {
		f.mu.Unlock()
		http.Error(w, "Storage unavailable", http.StatusServiceUnavailable)
		return
	}
	event = "login_verified"
	f.mu.Unlock()
	b := f.broker
	b.mu.Lock()
	var data browserOrderPage
	if strings.HasPrefix(f.target, "manage/") {
		p, ok := b.permissions.Lookup(f.session)
		allowed := ok && p.allowed && p.user == f.user && time.Now().Before(p.expires)
		audit.Record(b.manager.Logger, "permission_review_login_succeeded", audit.Actor{UserID: f.user, SessionID: f.session, ClientID: p.client}, "source_ip", b.oauth.ClientIP(r))
		data = browserOrderPage{Title: "Order approval settings", Account: f.user, ClientName: p.clientName, Manage: true, Allowed: allowed, Expiry: p.expires.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST")}
		for _, saved := range b.pending.All() {
			if saved.session == f.session && saved.user == f.user && saved.status == "approved_for_later" && time.Now().Before(saved.expires) {
				data.Saved = true
				data.ClientName = saved.clientName
			}
		}
	} else if p := b.pending.Get(f.target); p != nil && p.session == f.session && p.user == f.user {
		data = b.orderPageLocked(p)
		b.auditOrder(p, "order_review_login_succeeded", "outcome", "verified")
	} else {
		data = browserOrderPage{Title: "Approval unavailable", Message: "Ask your app to check the request status."}
	}
	b.mu.Unlock()
	data.CSRF = f.id + "." + f.secret
	data.FormAction = b.approvalURL(f.target)
	b.render(w, http.StatusOK, data)
}

func (b *BrowserApprovals) verifyReviewForm(w http.ResponseWriter, r *http.Request, target, session, user string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil || len(r.PostForm["csrf"]) != 1 {
		b.render(w, http.StatusForbidden, browserOrderPage{Title: "Review could not be verified", Message: "Open the approval link and sign in again. This attempt submitted nothing."})
		return false
	}
	id, _, ok := strings.Cut(r.PostForm.Get("csrf"), ".")
	pending, err := b.manager.SessionManager().Get(id)
	if !ok || err != nil || pending.Terminated || !time.Now().Before(pending.ExpiresAt) {
		b.render(w, http.StatusForbidden, browserOrderPage{Title: "Sign-in required", Message: "Sign in to Kite to review this request.", RetryURL: b.approvalURL(target)})
		return false
	}
	f, ok := pending.OAuthData.(*reviewLogin)
	if !ok {
		b.render(w, http.StatusForbidden, browserOrderPage{Title: "Review could not be verified", Message: "Open the approval link and sign in again. This attempt submitted nothing."})
		return false
	}
	f.mu.Lock()
	valid := f.broker == b && f.target == target && f.session == session && f.user == user && f.verified && time.Now().Before(f.expires) && validApprovalCSRF(r, f.id+"."+f.secret)
	f.mu.Unlock()
	if !valid {
		b.render(w, http.StatusForbidden, browserOrderPage{Title: "Review unavailable", Message: "This approval expired or we couldn't verify it. Ask your app to check the request status before starting another approval."})
	}
	return valid
}

func (b *BrowserApprovals) renderOrderOutcome(w http.ResponseWriter, p *browserOrder) {
	b.mu.Lock()
	data := b.orderPageLocked(p)
	b.mu.Unlock()
	b.render(w, http.StatusOK, data)
}

func (b *BrowserApprovals) reviewMetricOperation(target string) string {
	if strings.HasPrefix(target, "manage/") {
		return "permissions"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if p := b.pending.Get(target); p != nil {
		return p.operation
	}
	return "other"
}
