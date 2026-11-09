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
	"sort"
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
	trace                                            approvalTrace
	id, session, user, client, clientName, operation string
	snapshot, arguments                              []byte
	expires, retainUntil                             time.Time
	status                                           string
	result                                           *sdk.CallToolResult
	rememberRequested                                bool
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
	loginLimit  *oauth.RateLimiter
}

func NewBrowserApprovals(manager *kc.Manager, server *oauth.Server) *BrowserApprovals {
	page := template.Must(template.New("order_approval.html").ParseFS(templates.FS, "base.html", "brand_assets.html", "order_approval.html"))
	return &BrowserApprovals{pending: make(map[string]*browserOrder), permissions: make(map[string]clientOrderPermission), manager: manager, oauth: server, page: page, loginLimit: oauth.NewRateLimiter(30, time.Minute)}
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
		name = "Unnamed app"
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
		if p.allowed {
			b.metricEvent("permissions", "permission_expired")
		}
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
		b.expireOrderLocked(p, now)
		if !now.Before(p.retainUntil) && p.status != "submitting" {
			delete(b.pending, id)
		}
	}
	for id, p := range b.permissions {
		if !now.Before(p.expires) {
			if p.allowed {
				b.metricEvent("permissions", "permission_expired")
			}
			delete(b.permissions, id)
		}
	}
}

func (b *BrowserApprovals) request(ctx context.Context, req *sdk.CallToolRequest, operation string, details any) (*sdk.CallToolResult, error) {
	c, name, err := b.identity(ctx, req)
	if err != nil {
		if trace := traceFrom(ctx); trace != nil {
			trace.Method, trace.Outcome = "none", "unavailable"
			if trace.NativeAttempted {
				trace.Method = "native"
			}
		}
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
	p := &browserOrder{id: randomApprovalSecret(), session: c.SessionID, user: c.KiteUserID, client: c.ClientID, clientName: name, operation: operation, snapshot: snapshot, arguments: arguments, expires: expires, retainUntil: now.Add(15 * time.Minute), status: "approval_required"}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupLocked(now)
	active := 0
	for _, existing := range b.pending {
		if existing.session == c.SessionID && (existing.status == "approval_required" || existing.status == "approved_for_later") && now.Before(existing.expires) {
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
				b.expireOrderLocked(existing, now)
			}
			if existing.status != "approval_required" && existing.status != "approved_for_later" && existing.status != "submitting" && (oldest == nil || existing.retainUntil.Before(oldest.retainUntil)) {
				oldest = existing
			}
		}
		if oldest == nil {
			return approvalFailure("busy", "Too many order approvals. Finish an existing request before starting another; no transaction was executed."), nil
		}
		delete(b.pending, oldest.id)
	}
	if trace := traceFrom(ctx); trace != nil {
		p.trace = *trace
	}
	b.pending[p.id] = p
	b.metricEvent(operation, "created")
	data := map[string]any{"status": "approval_required", "approval_id": p.id, "approval_url": b.approvalURL(p.id), "expires_at": expires.Format(time.RFC3339), "message": "No order was submitted. Open the approval URL in any browser, sign in to the matching Kite account, then review and approve. Do not repeat the order request. After approval, use orders mode=approval_status with this approval_id to retrieve the outcome."}
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
		b.expireOrderLocked(p, time.Now())
	}
	data := map[string]any{"approval_id": id, "status": p.status, "result": p.result}
	trace := p.trace
	if outcome, ok := map[string]string{"approval_required": "required", "submitting": "approved", "declined": "declined", "expired": "expired"}[p.status]; ok {
		trace.Outcome = outcome
	}
	trace.add(data)
	permission, allowed := b.permissions[c.SessionID]
	allowed = allowed && permission.allowed && permission.client == c.ClientID && permission.user == c.KiteUserID && time.Now().Before(permission.expires)
	data["future_orders_allowed"] = allowed
	if p.status == "approved_for_later" {
		data["expires_at"] = p.expires.Format(time.RFC3339)
		data["permissions_url"] = b.manageURL(c.SessionID)
		data["message"] = "Approval saved for one future request with exactly the same order details. No order was placed. Wait for the user to ask to place it; do not repeat the mutation automatically."
	}
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
	b.mu.Lock()
	saved := b.savedCountLocked(c.SessionID, c.ClientID, c.KiteUserID)
	b.mu.Unlock()
	data["saved_order_approvals"] = saved
	if allowed || saved > 0 {
		data["permissions_url"] = b.manageURL(c.SessionID)
		data["message"] = "Share permissions_url as a clickable link labelled Ask me before every order. The user can revoke future order permission after signing in to Kite. Do not open or submit the page on their behalf."
	}
	return browserApprovalResult(data), nil
}

type browserOrderPage struct {
	Title, ClientName, Account, CSRF, FormAction, RetryURL, Message, ManageURL, Expiry, SavedExpiry, ApprovalExpiry, SubmitLabel, PermissionNote string
	Sections                                                                                                                                     []browserOrderSection
	Notes                                                                                                                                        []string
	SaveLater, Saved, Pending, Manage, Allowed                                                                                                   bool
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
	priority := map[string]int{"Symbol": 1, "Exchange": 2, "Side": 3, "Quantity": 4, "Order type": 5, "Price (₹)": 6, "Trigger price (₹)": 7, "Product": 8, "Validity": 9, "Variety": 10}
	for _, section := range sections {
		sort.SliceStable(section.Rows, func(i, j int) bool {
			left, right := priority[section.Rows[i].Label], priority[section.Rows[j].Label]
			if left == 0 {
				left = 100
			}
			if right == 0 {
				right = 100
			}
			return left < right
		})
	}
	return sections
}

func approvalHTTPHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
}

func (b *BrowserApprovals) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	approvalHTTPHeaders(w)
	purpose := "review"
	if strings.HasPrefix(r.URL.Path, "/order-approvals/manage/") {
		purpose = "permissions"
	}
	next := http.NewCrossOriginProtection().Handler(http.HandlerFunc(b.serveHTTP))
	b.manager.Metrics().HTTPMiddleware("approval_http", purpose, next).ServeHTTP(w, r)
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
	var expires time.Time
	if p != nil {
		expires = p.expires
	}
	b.mu.Unlock()
	if p == nil {
		b.render(w, http.StatusNotFound, browserOrderPage{Title: "Approval unavailable", Message: "This approval has expired or is no longer available. Ask your app to check the order status before starting a new request."})
		return
	}
	session, sessionErr := b.manager.SessionManager().Get(p.session)
	if sessionErr != nil || session.Credentials == nil || session.Credentials.UserID != p.user {
		b.render(w, http.StatusForbidden, browserOrderPage{Title: "Approval unavailable", Message: "This Kite connection has ended or changed account. No order was submitted."})
		return
	}
	if r.Method == http.MethodPost {
		if !b.verifyReviewForm(w, r, id, p.session, p.user) {
			return
		}
		b.decide(w, r, p)
		return
	}
	b.startReviewLogin(w, r, id, p.session, p.user, expires)
}

func (b *BrowserApprovals) orderPageLocked(p *browserOrder) browserOrderPage {
	if p.status == "approval_required" && !time.Now().Before(p.expires) {
		b.expireOrderLocked(p, time.Now())
	}
	data := browserOrderPage{Title: map[string]string{"orders_place": "Review order", "orders_modify": "Review order changes", "orders_cancel": "Review cancellation"}[p.operation], ClientName: p.clientName, Account: p.user, Sections: browserOrderSections(p.operation, p.snapshot), ApprovalExpiry: p.expires.In(time.FixedZone("IST", 19800)).Format("15:04 IST")}
	var fields map[string]any
	if json.Unmarshal(p.snapshot, &fields) == nil {
		data.Notes = tradeApprovalNotes(p.operation, fields)
	}
	if p.status == "approval_required" {
		data.Pending = true
		data.SaveLater = p.operation == "orders_place"
		data.SubmitLabel = map[string]string{"orders_place": "Place this order", "orders_modify": "Apply these changes", "orders_cancel": "Cancel this order"}[p.operation]
		session, _ := b.manager.SessionManager().Get(p.session)
		if session != nil {
			data.Expiry = session.ExpiresAt.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST")
			data.SavedExpiry = data.Expiry
		}
		data.Message = map[string]string{
			"orders_place":  "Check the details before placing this order.",
			"orders_modify": "Check the current order and the changes below.",
			"orders_cancel": "Cancelling this order does not undo trades already executed.",
		}[p.operation]
	} else {
		data.Title = map[string]string{"approved_for_later": "Approval saved. Order not placed", "submitted": "Request sent to Kite", "failed": "Check the order status", "declined": "Order not submitted", "expired": "Approval expired", "submitting": "Sending request to Kite"}[p.status]
		data.Message = map[string]string{"approved_for_later": "No order was placed. This approval can be used once, for the same order details, before it expires. Ask your app to place the order when you are ready. Saving approval does not schedule an order.", "submitted": "Kite accepted your request. This does not mean the order was executed. Ask your app to check the order status and trades. You can close this page.", "failed": "We couldn't confirm whether Kite accepted your request. Ask your app to check orders and trades before trying again.", "declined": "You declined this request. Nothing was submitted.", "expired": "This approval expired without submitting the order.", "submitting": "Ask your app to check the request status. Do not send this request again."}[p.status]
	}
	if p.status == "approved_for_later" {
		data.Saved = true
		data.SavedExpiry = p.expires.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST")
		data.ManageURL = b.manageURL(p.session)
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
	if permission, ok := b.permissions[p.session]; ok && permission.allowed && permission.client == p.client && permission.user == p.user && time.Now().Before(permission.expires) {
		data.Allowed = true
		data.ManageURL = b.manageURL(p.session)
		data.Expiry = permission.expires.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST")
	} else if p.rememberRequested && (p.status == "failed" || p.status == "submitted") {
		data.PermissionNote = "Future orders still require approval. This request did not allow orders without asking."
	}
	return data
}

func validApprovalCSRF(r *http.Request, csrf string) bool {
	return len(r.PostForm["csrf"]) == 1 && subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(csrf)) == 1
}

func (b *BrowserApprovals) decide(w http.ResponseWriter, r *http.Request, p *browserOrder) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid approval form", http.StatusForbidden)
		return
	}
	decision := r.PostForm.Get("decision")
	if len(r.PostForm["decision"]) != 1 || (decision != "approve" && decision != "decline" && decision != "save") || len(r.PostForm["remember"]) > 1 || (r.PostForm.Has("remember") && r.PostForm.Get("remember") != "yes") {
		http.Error(w, "Invalid approval decision", http.StatusBadRequest)
		return
	}
	if decision == "save" && (p.operation != "orders_place" || r.PostForm.Has("remember")) {
		http.Error(w, "Save approval only applies to this exact new order", http.StatusBadRequest)
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
		b.metricEvent(p.operation, "declined")
		b.mu.Unlock()
		b.renderOrderOutcome(w, p)
		return
	}
	if decision == "save" {
		deadline, err := b.manager.SessionExpiry(p.session)
		if err != nil {
			b.mu.Unlock()
			http.Error(w, "Connection expired", http.StatusForbidden)
			return
		}
		// Saving permission is terminal for this form. It never calls Kite.
		p.status = "approved_for_later"
		p.expires = deadline
		p.retainUntil = deadline.Add(15 * time.Minute)
		p.trace.Outcome = "approved_for_later"
		b.mu.Unlock()
		b.renderOrderOutcome(w, p)
		return
	}
	permissionGeneration := b.permissions[p.session].generation
	p.rememberRequested = r.PostForm.Get("remember") == "yes"
	b.metricEvent(p.operation, "submission_started")
	p.status = "submitting" // Single-use before fresh authentication and any API attempt.
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	// Keep expiry effective during order-history reads, credential renewal, and
	// the outbound request, rather than checking it only before execution.
	ctx, expire := context.WithDeadline(ctx, p.expires)
	defer expire()
	ctx = context.WithValue(ctx, browserApprovalsKey{}, b)
	ctx = context.WithValue(ctx, approvedBrowserOrderKey{}, approvedBrowserOrder{session: p.session, user: p.user, operation: p.operation, snapshot: p.snapshot, expires: p.expires})
	req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "orders", Arguments: json.RawMessage(p.arguments)}, Extra: &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": {p.session}}}}
	result, err := (&OrdersTool{}).Handler(b.manager)(ctx, req)
	if err != nil {
		result = NewToolResultError("Order submission could not be confirmed. Inspect orders and fills before requesting another submission.")
	}
	b.mu.Lock()
	// Errors before the guard, such as expired credentials, carry no approval
	// observation. Do not manufacture authorization success for those failures.
	p.trace.Outcome = "unavailable"
	if result != nil {
		encoded, _ := json.Marshal(result.StructuredContent)
		var observation approvalTrace
		if json.Unmarshal(encoded, &observation) == nil && observation.Outcome != "" {
			p.trace.Outcome = observation.Outcome
		}
		result = approvalResultWithTrace(result, p.trace)
	}
	p.result = result
	p.status = "failed"
	if result != nil && !result.IsError {
		p.status = "submitted"
		if r.PostForm.Get("remember") == "yes" && b.permissions[p.session].generation == permissionGeneration {
			if deadline, err := b.manager.SessionExpiry(p.session); err == nil {
				current := b.permissions[p.session]
				if !current.allowed || current.client != p.client || current.user != p.user || !time.Now().Before(current.expires) {
					b.metricEvent("permissions", "permission_granted")
				}
				b.permissions[p.session] = clientOrderPermission{client: p.client, user: p.user, clientName: p.clientName, expires: deadline, allowed: true, generation: permissionGeneration}
			}
		}
	}
	b.metricEvent(p.operation, p.status)
	observeApproval(b.manager.Metrics(), p.operation, result, p.trace)
	b.mu.Unlock()
	b.renderOrderOutcome(w, p)
}

func (b *BrowserApprovals) manage(w http.ResponseWriter, r *http.Request, sessionID string) {
	session, err := b.manager.SessionManager().Get(sessionID)
	if err != nil || session.Credentials == nil || session.Terminated || !time.Now().Before(session.ExpiresAt) {
		b.render(w, http.StatusForbidden, browserOrderPage{Title: "Connection unavailable", Message: "This Kite MCP connection has ended."})
		return
	}

	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if !b.verifyReviewForm(w, r, "manage/"+sessionID, sessionID, session.Credentials.UserID) {
			return
		}
		if len(r.PostForm["decision"]) != 1 || r.PostForm.Get("decision") != "revoke" {
			http.Error(w, "Invalid permission form", http.StatusForbidden)
			return
		}
		b.mu.Lock()
		permission := b.permissions[sessionID]
		if permission.allowed {
			b.metricEvent("permissions", "permission_revoked")
		}
		for _, saved := range b.pending {
			if saved.session == sessionID && saved.status == "approved_for_later" {
				saved.status = "declined"
			}
		}
		permission.allowed = false
		permission.generation++
		permission.expires = session.ExpiresAt
		b.permissions[sessionID] = permission
		b.mu.Unlock()
		b.render(w, http.StatusOK, browserOrderPage{Title: "Approval required for future orders", Message: "You must approve future orders from this app. Saved approvals have been removed. Existing orders have not been cancelled."})
		return
	}
	b.startReviewLogin(w, r, "manage/"+sessionID, sessionID, session.Credentials.UserID, time.Now().Add(tradeApprovalTimeout))
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

// Called under b.mu; repeats and status polling must not count expiry twice.
func (b *BrowserApprovals) expireOrderLocked(p *browserOrder, now time.Time) {
	if (p.status == "approval_required" || p.status == "approved_for_later") && !now.Before(p.expires) {
		p.status = "expired"
		b.metricEvent(p.operation, "expired")
	}
}

func (b *BrowserApprovals) metricEvent(operation, event string) {
	if m := b.manager.Metrics(); m != nil {
		m.RecordBrowserApprovalEvent(operation, event)
	}
}

// Saved approvals authorize one later exact match, never an automatic submission.
// Consume under the lock before execution, including uncertain/failed attempts.
func (b *BrowserApprovals) executeSaved(ctx context.Context, req *sdk.CallToolRequest, operation string, details any, execute func() (*sdk.CallToolResult, error)) (*sdk.CallToolResult, error, bool) {
	c, _, err := b.identity(ctx, req)
	if err != nil || operation != "orders_place" || ctx.Err() != nil {
		return nil, nil, false
	}
	snapshot, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return nil, nil, false
	}
	b.mu.Lock()
	b.cleanupLocked(time.Now())
	var selected *browserOrder
	for _, p := range b.pending {
		if p.status == "approved_for_later" && p.session == c.SessionID && p.client == c.ClientID && p.user == c.KiteUserID && p.operation == operation && bytes.Equal(p.snapshot, snapshot) {
			selected = p
			break
		}
	}
	if selected == nil {
		b.mu.Unlock()
		return nil, nil, false
	}
	selected.status = "submitting"
	b.mu.Unlock()
	traceMethod(ctx, "remembered_permission", "approved", "")
	var result *sdk.CallToolResult
	if ctx.Err() != nil || !time.Now().Before(selected.expires) {
		result = approvalFailure("timeout", "Saved approval expired; no order was submitted.")
	} else {
		result, err = execute()
	}
	b.mu.Lock()
	if trace := traceFrom(ctx); trace != nil {
		selected.trace = *trace
		result = approvalResultWithTrace(result, *trace)
	}
	selected.result = result
	selected.status = "failed"
	if err == nil && result != nil && !result.IsError {
		selected.status = "submitted"
	}
	selected.retainUntil = time.Now().Add(15 * time.Minute)
	b.mu.Unlock()
	return result, err, true
}

func (b *BrowserApprovals) savedCountLocked(session, client, user string) int {
	count := 0
	for _, p := range b.pending {
		if p.session == session && p.client == client && p.user == user && p.status == "approved_for_later" && time.Now().Before(p.expires) {
			count++
		}
	}
	return count
}
