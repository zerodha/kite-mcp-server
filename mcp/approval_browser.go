package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/templates"
	"github.com/zerodha/kite-mcp-server/oauth"
)

type browserApprovalsKey struct{}
type approvedBrowserOrderKey struct{}
type approvedBrowserOrder struct {
	session, user, operation string
	snapshot                 []byte
	expires                  time.Time
}

type browserOrder struct {
	id, session, user, client, clientName, operation, csrf string
	snapshot, arguments                                    []byte
	expires, retainUntil                                   time.Time
	status                                                 string
	result                                                 *sdk.CallToolResult
	rememberRequested                                      bool
}

type clientOrderPermission struct {
	client, user, clientName string
	expires                  time.Time
	allowed                  bool
	generation               uint64
}

// BrowserApprovals keeps approval state separate from caller-controlled tool
// arguments. All state ends with the application grant or server process.
type BrowserApprovals struct {
	mu          sync.Mutex
	pending     map[string]*browserOrder
	permissions map[string]clientOrderPermission
	manager     *kc.Manager
	oauth       *oauth.Server
	page        *template.Template
}

func NewBrowserApprovals(manager *kc.Manager, server *oauth.Server) *BrowserApprovals {
	page := template.Must(template.New("order_approval.html").ParseFS(templates.FS, "base.html", "brand_assets.html", "order_approval.html"))
	return &BrowserApprovals{pending: make(map[string]*browserOrder), permissions: make(map[string]clientOrderPermission), manager: manager, oauth: server, page: page}
}

func (b *BrowserApprovals) Attach(server *sdk.Server) {
	server.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			return next(context.WithValue(ctx, browserApprovalsKey{}, b), method, req)
		}
	})
}

func browserApprovalsFrom(ctx context.Context) *BrowserApprovals {
	b, _ := ctx.Value(browserApprovalsKey{}).(*BrowserApprovals)
	return b
}

func (b *BrowserApprovals) identity(ctx context.Context, req *sdk.CallToolRequest) (*oauth.TokenClaims, string, error) {
	c := oauth.ClaimsFromContext(ctx)
	if c == nil || c.ClientID == "" || c.SessionID == "" || c.KiteUserID == "" || c.SessionID != GetSessionID(req) {
		return nil, "", fmt.Errorf("verified client identity unavailable")
	}
	if _, err := b.manager.SessionExpiry(c.SessionID); err != nil {
		return nil, "", err
	}
	session, err := b.manager.SessionManager().Get(c.SessionID)
	if err != nil || session.Credentials == nil || session.Credentials.UserID != c.KiteUserID {
		return nil, "", fmt.Errorf("account does not match authenticated grant")
	}
	name, err := b.oauth.ClientName(c.ClientID)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(name) == "" {
		name = "Unnamed MCP client"
	}
	return c, name, nil
}

func (b *BrowserApprovals) trusted(ctx context.Context, req *sdk.CallToolRequest) bool {
	c, _, err := b.identity(ctx, req)
	if err != nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.permissions[c.SessionID]
	if ok && !time.Now().Before(p.expires) {
		delete(b.permissions, c.SessionID)
		return false
	}
	return ok && p.allowed && p.client == c.ClientID && p.user == c.KiteUserID
}

func randomApprovalSecret() string {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}

func (b *BrowserApprovals) cleanupLocked(now time.Time) {
	for id, p := range b.pending {
		if !now.Before(p.retainUntil) && p.status != "submitting" {
			delete(b.pending, id)
		}
	}
	for id, p := range b.permissions {
		if !now.Before(p.expires) {
			delete(b.permissions, id)
		}
	}
}

func (b *BrowserApprovals) request(ctx context.Context, req *sdk.CallToolRequest, operation string, details any) (*sdk.CallToolResult, error) {
	c, name, err := b.identity(ctx, req)
	if err != nil {
		return approvalFailure("unsupported_client", "Browser approval requires a verified client connection. Reconnect and sign in through Kite. No transaction was executed."), nil
	}
	snapshot, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return nil, err
	}
	arguments, err := json.Marshal(GetArguments(req))
	if err != nil {
		return nil, err
	}
	if len(snapshot)+len(arguments) > 64<<10 {
		return approvalFailure("invalid_details", "Order details are too large; no transaction was executed."), nil
	}
	deadline, err := b.manager.SessionExpiry(c.SessionID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	expires := now.Add(tradeApprovalTimeout)
	if deadline.Before(expires) {
		expires = deadline
	}
	p := &browserOrder{id: randomApprovalSecret(), csrf: randomApprovalSecret(), session: c.SessionID, user: c.KiteUserID, client: c.ClientID, clientName: name, operation: operation, snapshot: snapshot, arguments: arguments, expires: expires, retainUntil: now.Add(15 * time.Minute), status: "approval_required"}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupLocked(now)
	active := 0
	for _, existing := range b.pending {
		if existing.session == c.SessionID && existing.status == "approval_required" && now.Before(existing.expires) {
			active++
		}
	}
	if active >= maxPendingTradeApprovalsPerSession {
		return approvalFailure("busy", "Too many order approvals. Finish an existing request before starting another; no transaction was executed."), nil
	}
	// Completed outcomes are retained for lookup, but must not consume every
	// slot needed by new reviews. Reclaim the oldest terminal record at capacity.
	if len(b.pending) >= maxPendingTradeApprovals {
		var oldest *browserOrder
		for _, existing := range b.pending {
			if existing.status == "approval_required" && !now.Before(existing.expires) {
				existing.status = "expired"
			}
			if existing.status != "approval_required" && existing.status != "submitting" && (oldest == nil || existing.retainUntil.Before(oldest.retainUntil)) {
				oldest = existing
			}
		}
		if oldest == nil {
			return approvalFailure("busy", "Too many order approvals. Finish an existing request before starting another; no transaction was executed."), nil
		}
		delete(b.pending, oldest.id)
	}
	b.pending[p.id] = p
	data := map[string]any{"status": "approval_required", "approval_id": p.id, "approval_url": b.approvalURL(p.id), "expires_at": expires.Format(time.RFC3339), "message": "No order was submitted. Open the approval URL in the browser used to sign in to Kite MCP. Review and approve there. Do not repeat the order request. After approval, use orders mode=approval_status with this approval_id to retrieve the outcome."}
	return browserApprovalResult(data), nil
}

func browserApprovalResult(data map[string]any) *sdk.CallToolResult {
	encoded, _ := json.Marshal(data)
	result := NewToolResultText(string(encoded))
	result.StructuredContent = data
	return result
}

func (b *BrowserApprovals) approvalURL(id string) string {
	return b.oauth.IssuerURL() + "/order-approvals/" + id
}
func (b *BrowserApprovals) manageURL(session string) string {
	return b.approvalURL("manage/" + url.PathEscape(session))
}

func (b *BrowserApprovals) status(ctx context.Context, req *sdk.CallToolRequest, id string) (*sdk.CallToolResult, error) {
	c, _, err := b.identity(ctx, req)
	if err != nil {
		return NewToolResultError("Sign in to check order approvals."), nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupLocked(time.Now())
	p, ok := b.pending[id]
	if !ok || p.session != c.SessionID || p.client != c.ClientID || p.user != c.KiteUserID {
		return NewToolResultError("Order approval not found for this connection."), nil
	}
	if p.status == "approval_required" && !time.Now().Before(p.expires) {
		p.status = "expired"
	}
	data := map[string]any{"approval_id": id, "status": p.status, "result": p.result}
	permission, allowed := b.permissions[c.SessionID]
	allowed = allowed && permission.allowed && permission.client == c.ClientID && permission.user == c.KiteUserID && time.Now().Before(permission.expires)
	data["future_orders_allowed"] = allowed
	if allowed {
		data["permissions_url"] = b.manageURL(c.SessionID)
	}
	return browserApprovalResult(data), nil
}

func (b *BrowserApprovals) permissionStatus(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
	c, _, err := b.identity(ctx, req)
	if err != nil {
		return NewToolResultError("Sign in to manage order permissions."), nil
	}
	allowed := b.trusted(ctx, req)
	data := map[string]any{"future_orders_allowed": allowed, "message": "This connection already requires approval for order requests. No permission change is needed."}
	if allowed {
		data["permissions_url"] = b.manageURL(c.SessionID)
		data["message"] = "Share permissions_url as a clickable link labelled Require approval again. The user can revoke future order permission in their sign-in browser. Do not open or submit the page on their behalf."
	}
	return browserApprovalResult(data), nil
}

type browserOrderPage struct {
	Title, ClientName, Account, CSRF, Message, ManageURL, Expiry, ApprovalExpiry, SubmitLabel, PermissionNote string
	Sections                                                                                                  []browserOrderSection
	Pending, Manage, Allowed                                                                                  bool
}
type browserOrderSection struct {
	Title string
	Rows  []browserOrderRow
}
type browserOrderRow struct{ Label, Value string }

func browserOrderSections(operation string, snapshot []byte) []browserOrderSection {
	// Use the same escaped display as native approvals; parse only the formatter's
	// fixed labels, not raw instrument strings (which have control characters quoted).
	message := tradeApprovalForm(operation, snapshot).Message
	lines := strings.Split(message, "\n")
	sections := []browserOrderSection{{Title: "Order details"}}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Order details:" {
			continue
		}
		if trimmed == "Current order:" || trimmed == "Requested changes:" {
			if len(sections[len(sections)-1].Rows) == 0 {
				sections[len(sections)-1].Title = strings.TrimSuffix(trimmed, ":")
			} else {
				sections = append(sections, browserOrderSection{Title: strings.TrimSuffix(trimmed, ":")})
			}
			continue
		}
		label, value, ok := strings.Cut(trimmed, ": ")
		if ok {
			sections[len(sections)-1].Rows = append(sections[len(sections)-1].Rows, browserOrderRow{Label: label, Value: value})
		}
	}
	return sections
}

func approvalHTTPHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
}

func (b *BrowserApprovals) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	approvalHTTPHeaders(w)
	http.NewCrossOriginProtection().Handler(http.HandlerFunc(b.serveHTTP)).ServeHTTP(w, r)
}

func (b *BrowserApprovals) render(w http.ResponseWriter, status int, data browserOrderPage) {
	if err := templates.RenderPage(w, status, b.page, data); err != nil {
		b.manager.Logger.Error("approval page render failed", "error", err)
	}
}

func (b *BrowserApprovals) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/order-approvals/")
	if strings.HasPrefix(id, "manage/") {
		b.manage(w, r, strings.TrimPrefix(id, "manage/"))
		return
	}
	if len(id) != 64 {
		http.NotFound(w, r)
		return
	}
	b.mu.Lock()
	b.cleanupLocked(time.Now())
	p := b.pending[id]
	b.mu.Unlock()
	if p == nil {
		b.render(w, http.StatusNotFound, browserOrderPage{Title: "Approval unavailable", Message: "This order approval has expired or is no longer available. Return to your client and check the order status before starting another request."})
		return
	}
	session, sessionErr := b.manager.SessionManager().Get(p.session)
	if sessionErr != nil || session.Credentials == nil || session.Credentials.UserID != p.user || b.oauth.VerifyOrderBrowserSession(r, p.session, p.user) != nil {
		b.render(w, http.StatusForbidden, browserOrderPage{Title: "Sign-in required", Message: "Open this link in the browser you used to connect this client to Kite MCP. If you signed in elsewhere, reconnect the client using this browser. No order was submitted by opening this link."})
		return
	}
	if r.Method == http.MethodPost {
		b.decide(w, r, p)
		return
	}
	b.mu.Lock()
	data := b.orderPageLocked(p)
	b.mu.Unlock()
	b.render(w, http.StatusOK, data)
}

func (b *BrowserApprovals) orderPageLocked(p *browserOrder) browserOrderPage {
	if p.status == "approval_required" && !time.Now().Before(p.expires) {
		p.status = "expired"
	}
	data := browserOrderPage{Title: "Review order", ClientName: p.clientName, Account: p.user, CSRF: p.csrf, Sections: browserOrderSections(p.operation, p.snapshot), ApprovalExpiry: p.expires.In(time.FixedZone("IST", 19800)).Format("15:04:05 IST")}
	if p.status == "approval_required" {
		data.Pending = true
		data.SubmitLabel = map[string]string{"orders_place": "Place this order", "orders_modify": "Apply these changes", "orders_cancel": "Cancel this order"}[p.operation]
		session, _ := b.manager.SessionManager().Get(p.session)
		if session != nil {
			data.Expiry = session.ExpiresAt.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST")
		}
		data.Message = map[string]string{
			"orders_place":  "Confirming submits this order to Kite once.",
			"orders_modify": "Only the requested changes will be submitted. Other fields stay unchanged.",
			"orders_cancel": "Cancelling this order does not reverse existing fills.",
		}[p.operation]
	} else {
		data.Title = map[string]string{"submitted": "Order request submitted", "failed": "Order request needs attention", "declined": "Order not submitted", "expired": "Approval expired", "submitting": "Order request in progress"}[p.status]
		data.Message = map[string]string{"submitted": "Kite accepted the request. Check order status and fills in your client; exchange execution is not guaranteed.", "failed": "The request could not be confirmed. Check the result in your client and inspect orders and fills before requesting another submission.", "declined": "You declined this request. No transaction was submitted.", "expired": "This approval expired without submitting the order.", "submitting": "This request is already being processed. Do not submit it again. Check its status in your client."}[p.status]
	}
	if p.status == "submitted" && p.result != nil {
		for _, content := range p.result.Content {
			if text, ok := content.(*sdk.TextContent); ok {
				var result map[string]any
				if json.Unmarshal([]byte(text.Text), &result) == nil {
					if id, ok := result["order_id"].(string); ok && id != "" {
						data.Sections = append(data.Sections, browserOrderSection{Title: "Submission result", Rows: []browserOrderRow{{Label: "Order ID", Value: id}}})
					}
				}
			}
		}
	}
	if permission, ok := b.permissions[p.session]; ok && permission.allowed && permission.client == p.client && time.Now().Before(permission.expires) {
		data.ManageURL = b.manageURL(p.session)
		data.Expiry = permission.expires.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST")
	} else if p.rememberRequested && (p.status == "failed" || p.status == "submitted") {
		data.PermissionNote = "Future orders still require approval. No future-order permission was enabled by this request."
	}
	return data
}

func validApprovalCSRF(r *http.Request, csrf string) bool {
	return len(r.PostForm["csrf"]) == 1 && subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(csrf)) == 1
}

func (b *BrowserApprovals) decide(w http.ResponseWriter, r *http.Request, p *browserOrder) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil || !validApprovalCSRF(r, p.csrf) {
		http.Error(w, "Invalid approval form", http.StatusForbidden)
		return
	}
	decision := r.PostForm.Get("decision")
	if len(r.PostForm["decision"]) != 1 || (decision != "approve" && decision != "decline") || len(r.PostForm["remember"]) > 1 || (r.PostForm.Has("remember") && r.PostForm.Get("remember") != "yes") {
		http.Error(w, "Invalid approval decision", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	if p.status != "approval_required" || !time.Now().Before(p.expires) {
		data := b.orderPageLocked(p)
		b.mu.Unlock()
		b.render(w, http.StatusOK, data)
		return
	}
	if decision == "decline" {
		p.status = "declined"
		b.mu.Unlock()
		http.Redirect(w, r, b.approvalURL(p.id), http.StatusSeeOther)
		return
	}
	permissionGeneration := b.permissions[p.session].generation
	p.rememberRequested = r.PostForm.Get("remember") == "yes"
	p.status = "submitting" // Single-use before fresh authentication and any API attempt.
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, browserApprovalsKey{}, b)
	ctx = context.WithValue(ctx, approvedBrowserOrderKey{}, approvedBrowserOrder{session: p.session, user: p.user, operation: p.operation, snapshot: p.snapshot, expires: p.expires})
	req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "orders", Arguments: json.RawMessage(p.arguments)}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": {p.session}}}}
	result, err := (&OrdersTool{}).Handler(b.manager)(ctx, req)
	if err != nil {
		result = NewToolResultError("Order submission could not be confirmed. Inspect orders and fills before requesting another submission.")
	}
	b.mu.Lock()
	p.result = result
	p.status = "failed"
	if result != nil && !result.IsError {
		p.status = "submitted"
		if r.PostForm.Get("remember") == "yes" && b.permissions[p.session].generation == permissionGeneration {
			if deadline, err := b.manager.SessionExpiry(p.session); err == nil {
				b.permissions[p.session] = clientOrderPermission{client: p.client, user: p.user, clientName: p.clientName, expires: deadline, allowed: true, generation: permissionGeneration}
			}
		}
	}
	b.mu.Unlock()
	http.Redirect(w, r, b.approvalURL(p.id), http.StatusSeeOther)
}

func (b *BrowserApprovals) manage(w http.ResponseWriter, r *http.Request, sessionID string) {
	session, err := b.manager.SessionManager().Get(sessionID)
	if err != nil || session.Credentials == nil || b.oauth.VerifyOrderBrowserSession(r, sessionID, session.Credentials.UserID) != nil {
		b.render(w, http.StatusForbidden, browserOrderPage{Title: "Sign-in required", Message: "Open this page in the browser used to sign in to this Kite MCP connection."})
		return
	}
	csrf := b.oauth.OrderBrowserCSRF(r, sessionID)
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil || csrf == "" || !validApprovalCSRF(r, csrf) || r.PostForm.Get("decision") != "revoke" {
			http.Error(w, "Invalid permission form", http.StatusForbidden)
			return
		}
		b.mu.Lock()
		permission := b.permissions[sessionID]
		permission.allowed = false
		permission.generation++
		permission.expires = session.ExpiresAt
		b.permissions[sessionID] = permission
		b.mu.Unlock()
		http.Redirect(w, r, b.manageURL(sessionID), http.StatusSeeOther)
		return
	}
	b.mu.Lock()
	permission, allowed := b.permissions[sessionID]
	allowed = allowed && permission.allowed && permission.user == session.Credentials.UserID && time.Now().Before(permission.expires)
	b.mu.Unlock()
	b.render(w, http.StatusOK, browserOrderPage{Title: "Future order approvals", Account: session.Credentials.UserID, ClientName: permission.clientName, Expiry: permission.expires.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST"), Manage: true, Allowed: allowed, CSRF: csrf, Message: "This permission applies only to this Kite MCP login session. Revoking it restores approval for future order requests and does not cancel submitted orders."})
}

// Only server-held browser state can reach this path. Re-read current order
// history and compare the normalized snapshot before executing the stored call.
func executeBrowserApproved(ctx context.Context, req *sdk.CallToolRequest, operation string, details any, execute func() (*sdk.CallToolResult, error)) (*sdk.CallToolResult, error, bool) {
	approved, ok := ctx.Value(approvedBrowserOrderKey{}).(approvedBrowserOrder)
	if !ok {
		return nil, nil, false
	}
	if browser := browserApprovalsFrom(ctx); browser != nil {
		session, err := browser.manager.SessionManager().Get(approved.session)
		if err != nil || session.Credentials == nil || session.Credentials.UserID != approved.user {
			return approvalFailure("invalid_state", "The account no longer matches this approval; no transaction was executed."), nil, true
		}
	}
	current, err := json.MarshalIndent(details, "", "  ")
	if err != nil || approved.session != GetSessionID(req) || approved.operation != operation || !bytes.Equal(current, approved.snapshot) {
		return approvalFailure("parameters_changed", "Order details changed after the approval page was created. No transaction was executed. Request a new review."), nil, true
	}
	if !time.Now().Before(approved.expires) || ctx.Err() != nil {
		return approvalFailure("timeout", "Approval expired before submission; no transaction was executed."), nil, true
	}
	result, err := execute()
	return result, err, true
}
