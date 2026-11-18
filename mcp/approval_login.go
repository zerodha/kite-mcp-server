package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"hash/fnv"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zerodha/kite-mcp-server/internal/audit"
	"github.com/zerodha/kite-mcp-server/kc"
)

// reviewLogin is sealed callback state until sign-in succeeds, then a
// temporary server-held proof for one exact review. Neither
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

func (f *reviewLogin) ReviewAuthorization() (string, time.Time) {
	return f.session, f.expires
}

func (b *BrowserApprovals) startReviewLogin(w http.ResponseWriter, r *http.Request, target, session, user string, expires time.Time) {
	if deadline := time.Now().Add(tradeApprovalTimeout); deadline.Before(expires) {
		expires = deadline
	}
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
	flow := &reviewLogin{broker: b, target: target, session: session, user: user, state: randomApprovalSecret(), secret: randomApprovalSecret(), previous: kc.CredentialSnapshot{Credentials: &kc.KiteCredentials{UserID: user}, Generation: kc.SnapshotCredentials(current.Credentials).Generation}.Restore(), expires: expires}
	_, snapshot := flow.SessionData()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		http.Error(w, "Sign-in unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := b.manager.SessionSigner().SealReviewAuthorization(encoded)
	if err != nil {
		b.render(w, http.StatusServiceUnavailable, browserOrderPage{Title: "Sign-in unavailable", Message: "Wait a few minutes, then open the approval link again."})
		return
	}
	flow.id = id
	loginURL, err := b.manager.GenerateLoginURL(id, flow.state)
	if err != nil {
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
	}
	f.mu.Unlock()
	if !valid {
		f.broker.render(w, http.StatusForbidden, browserOrderPage{Title: "Sign-in link unavailable", Message: "This link has expired or was already used. Ask your app to check the request status before starting another approval."})
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), f.expires)
	defer cancel()
	fresh, err := f.broker.completeReviewLogin(ctx, f, q.Get("request_token"))
	if err != nil || fresh.UserID != f.user {
		reason := "account_mismatch"
		if err != nil {
			reason = "token_exchange_failed"
		}
		audit.Record(f.broker.manager.Logger, "order_review_login_failed", audit.Actor{SessionID: f.session}, "expected_user_id", f.user, "approval_ref", audit.Ref(f.target), "reason", reason, "source_ip", f.broker.oauth.ClientIP(r))
		f.broker.render(w, http.StatusForbidden, browserOrderPage{Title: "Sign-in could not be verified", Account: f.user, Message: "Sign in to the Kite account shown above to review this request. No request was sent to Kite.", RetryURL: f.broker.approvalURL(f.target)})
		return
	}
	if ctx.Err() != nil || !time.Now().Before(f.expires) {
		f.broker.render(w, http.StatusForbidden, browserOrderPage{Title: "Review unavailable", Message: "The login changed or this approval expired. No request was sent to Kite. Ask your app to check the request status."})
		return
	}
	f.mu.Lock()
	f.verified = true
	proofID, err := f.broker.manager.SessionManager().GeneratePendingAuthorization(f)
	if err != nil {
		f.mu.Unlock()
		if errors.Is(err, kc.ErrPendingAuthorizationLimit) {
			f.broker.render(w, http.StatusTooManyRequests, browserOrderPage{Title: "Too many open reviews", Message: "Finish an existing review, then reopen this link and sign in again."})
		} else {
			http.Error(w, "Storage unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	f.id = proofID
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
	if valid {
		// Claim under the flow lock before any decision can run. Deleting the
		// persisted session makes the claim survive a restart as well.
		f.verified = false
		if _, err := b.manager.SessionManager().Terminate(f.id); err != nil {
			f.mu.Unlock()
			http.Error(w, "Storage unavailable", http.StatusServiceUnavailable)
			return false
		}
	}
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

var errReviewGrantChanged = errors.New("review grant changed")

// Serialize credential exchange per grant so replayed or concurrent callbacks
// cannot exchange the same sign-in twice. This fixed lock array holds no flows.
func (b *BrowserApprovals) completeReviewLogin(ctx context.Context, f *reviewLogin, token string) (*kc.KiteCredentials, error) {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(f.session))
	lock := &b.loginLocks[hash.Sum32()%uint32(len(b.loginLocks))]
	lock.Lock()
	defer lock.Unlock()
	current, err := b.manager.SessionManager().Get(f.session)
	if err != nil || current.Terminated || current.Credentials == nil || current.Credentials.UserID != f.user || !time.Now().Before(current.ExpiresAt) || kc.SnapshotCredentials(current.Credentials).Generation != kc.SnapshotCredentials(f.previous).Generation {
		return nil, errReviewGrantChanged
	}
	fresh, err := b.manager.CompleteLoginContext(ctx, token)
	if err != nil || fresh.UserID != f.user {
		return fresh, err
	}
	if ctx.Err() != nil || !time.Now().Before(f.expires) || b.manager.SessionManager().ReplaceApprovalCredentials(f.session, f.previous, fresh) != nil {
		return nil, errReviewGrantChanged
	}
	return fresh, nil
}
