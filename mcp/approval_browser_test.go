package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/app/metrics"
	"github.com/zerodha/kite-mcp-server/internal/storage"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/oauth"
)

type reviewTestIdentity struct{ user string }

type browserBearerTransport struct{ token string }

func (t browserBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(copy)
}

type browserApprovalFixture struct {
	b              *BrowserApprovals
	manager        *kc.Manager
	calls          atomic.Int32
	failMutations  atomic.Bool
	blockMutations atomic.Bool
	blockHistory   atomic.Bool
	historyStarted chan struct{}
	historyStopped chan struct{}
	started        chan struct{}
	release        chan struct{}
}

func newBrowserApprovalFixture(t *testing.T, metricManagers ...*metrics.Manager) *browserApprovalFixture {
	return newBrowserApprovalFixtureWithState(t, nil, metricManagers...)
}

func newBrowserApprovalFixtureWithState(t *testing.T, state storage.State, metricManagers ...*metrics.Manager) *browserApprovalFixture {
	f := &browserApprovalFixture{started: make(chan struct{}), release: make(chan struct{}), historyStarted: make(chan struct{}), historyStopped: make(chan struct{})}
	f.manager = approvalTestManagerWithState(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/session/token" {
			_ = r.ParseForm()
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]string{"user_id": r.PostForm.Get("request_token"), "access_token": "fresh-access"}})
			return
		}
		if r.Method == http.MethodGet {
			if f.blockHistory.Load() {
				close(f.historyStarted)
				<-r.Context().Done()
				close(f.historyStopped)
				return
			}
			_, _ = w.Write([]byte(`{"status":"success","data":[{"order_id":"order-1","variety":"regular","status":"OPEN","order_type":"LIMIT","quantity":1}]}`))
			return
		}
		f.calls.Add(1)
		if f.blockMutations.Load() {
			close(f.started)
			<-f.release
		}
		if f.failMutations.Load() {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"order_id":"order-1"}}`))
	}, state, metricManagers...)
	s := oauth.New(oauth.Config{State: state, Issuer: "https://mcp.example", JWTSecret: []byte(strings.Repeat("s", 32)), SessionExpiry: f.manager.SessionExpiry})
	require.NoError(t, s.RestoreState())
	f.b = NewBrowserApprovals(f.manager, s)
	require.NoError(t, f.b.RestoreState())
	return f
}

func (f *browserApprovalFixture) connect(t *testing.T, session, name, version string, options ...*sdk.ClientOptions) (*sdk.ClientSession, *reviewTestIdentity) {
	t.Helper()
	if session != "approval-test" {
		_, _, err := f.manager.SessionManager().GetOrCreate(session)
		require.NoError(t, err)
		require.NoError(t, f.manager.SessionManager().UpdateCredentials(session, &kc.KiteCredentials{UserID: "user", AccessToken: "test-access", ExpiresAt: time.Now().Add(time.Hour)}))
	}
	clientID, err := f.b.oauth.RegisterClient([]string{"https://client.example/callback"}, name)
	require.NoError(t, err)
	verifier := strings.Repeat("v", 43)
	hash := sha256.Sum256([]byte(verifier))
	code, err := f.b.oauth.CreateAuthCode(clientID, "https://client.example/callback", "user", session, base64.RawURLEncoding.EncodeToString(hash[:]))
	require.NoError(t, err)
	token, err := f.b.oauth.ExchangeCode(&oauth.TokenRequest{Code: code, ClientID: clientID, RedirectURI: "https://client.example/callback", CodeVerifier: verifier})
	require.NoError(t, err)
	server := NewServer(&sdk.Implementation{Name: "server", Version: "1"})
	f.b.Attach(server)
	registerOperationMetrics(server, f.manager.Metrics())
	tool := &OrdersTool{}
	server.AddTool(tool.Definition(), sdk.ToolHandler(tool.Handler(f.manager)))
	endpoint := httptest.NewServer(f.b.oauth.Middleware(NewHTTPHandler(server, nil)))
	t.Cleanup(endpoint.Close)
	var option *sdk.ClientOptions
	if len(options) > 0 {
		option = options[0]
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "Spoofed client name", Version: "1"}, option)
	cs, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: browserBearerTransport{token}}}, &sdk.ClientSessionOptions{ProtocolVersion: version})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs, &reviewTestIdentity{user: "user"}
}

func browserOrderArgs() map[string]any {
	return map[string]any{"mode": "place", "exchange": "NSE", "tradingsymbol": "INFY", "transaction_type": "BUY", "quantity": 1, "product": "CNC", "order_type": "LIMIT", "price": 1500}
}
func callBrowserOrder(t *testing.T, cs *sdk.ClientSession, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "orders", Arguments: args})
	require.NoError(t, err)
	if !result.IsError && len(result.InputRequests) == 0 {
		validateStructuredOutput(t, (&OrdersTool{}).Definition(), result)
	}
	return result
}
func browserApprovalID(t *testing.T, result *sdk.CallToolResult) string {
	t.Helper()
	require.False(t, result.IsError)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].(*sdk.TextContent).Text), &data))
	require.Equal(t, "approval_required", data["status"])
	return data["approval_id"].(string)
}
func browserPage(t *testing.T, b *BrowserApprovals, id string, identity *reviewTestIdentity) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", b.approvalURL(id), nil)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	require.Empty(t, w.Result().Cookies(), "approval flow must not set cookies")
	if identity == nil || w.Code != http.StatusSeeOther {
		return w
	}
	login, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	query, err := url.ParseQuery(login.Query().Get("redirect_params"))
	require.NoError(t, err)
	query.Set("request_token", identity.user)
	r = httptest.NewRequest("GET", "https://mcp.example/callback?"+query.Encode(), nil)
	// Independent test browser peers avoid sharing the global callback limiter.
	digest := sha256.Sum256([]byte(query.Get("session_id")))
	r.RemoteAddr = fmt.Sprintf("[2001:db8:%x:%x::1]:1234", digest[:2], digest[2:4])
	w = httptest.NewRecorder()
	oauth.NewHandlers(b.oauth, b.manager, b.manager.Logger).HandleCallback(w, r)
	require.Empty(t, w.Result().Cookies(), "review sign-in must not set cookies")
	return w
}

func pageCSRF(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	m := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	require.Len(t, m, 2)
	return m[1]
}
func postBrowserDecision(b *BrowserApprovals, id string, _ *reviewTestIdentity, form url.Values, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", b.approvalURL(id), strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}

func TestBrowserApprovalWithoutElicitation(t *testing.T) {
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(version, func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			cs, identity := f.connect(t, "approval-test", "Pi <client>", version)
			require.Contains(t, cs.InitializeResult().Instructions, "status=approval_required")
			require.Contains(t, cs.InitializeResult().Instructions, "Do not open or approve it for the user")
			pending := callBrowserOrder(t, cs, browserOrderArgs())
			expiry, err := time.Parse(time.RFC3339, approvalObservation(t, pending)["expires_at"].(string))
			require.NoError(t, err)
			require.WithinDuration(t, time.Now().Add(5*time.Minute), expiry, 2*time.Second)
			id := browserApprovalID(t, pending)
			require.Zero(t, f.calls.Load())
			require.Equal(t, http.StatusSeeOther, browserPage(t, f.b, id, nil).Code, "link alone cannot approve or reveal order details")
			page := browserPage(t, f.b, id, identity)
			csrf := pageCSRF(t, page)
			require.Contains(t, page.Body.String(), "Pi &lt;client&gt;")
			require.NotContains(t, page.Body.String(), "Spoofed client name")
			require.NotRegexp(t, `<input[^>]*\bchecked(?:\s|=|>)`, page.Body.String())
			require.Contains(t, page.Body.String(), "INFY")
			require.NotContains(t, page.Body.String(), "Ask me before every order", "revocation link appears only after permission is enabled")
			require.Equal(t, "no-store", page.Header().Get("Cache-Control"))
			if path := os.Getenv("KITE_APPROVAL_PREVIEW_PATH"); path != "" {
				require.NoError(t, os.WriteFile(path, page.Body.Bytes(), 0600))
			}
			form := url.Values{"csrf": {csrf}, "decision": {"approve"}}
			require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, identity, form, "").Code)
			require.EqualValues(t, 1, f.calls.Load())
			require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, identity, form, "").Code)
			require.EqualValues(t, 1, f.calls.Load(), "browser refresh must not replay")
			status := callBrowserOrder(t, cs, map[string]any{"mode": "approval_status", "approval_id": id})
			require.Contains(t, status.Content[0].(*sdk.TextContent).Text, `"status":"submitted"`)
			browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
			require.EqualValues(t, 1, f.calls.Load(), "unchecked permission requires another approval")
		})
	}
}

func TestUnavailableNativeApprovalFallsBackWithoutAnotherOrderCall(t *testing.T) {
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		for _, scenario := range []string{"transport-error", "malformed-response", "decline", "cancel", "negative"} {
			t.Run(version+"/"+scenario, func(t *testing.T) {
				f := newBrowserApprovalFixture(t)
				cs, identity := f.connect(t, "approval-test", "Approval-capable client", version, &sdk.ClientOptions{ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
					switch scenario {
					case "transport-error":
						return nil, fmt.Errorf("native form unavailable")
					case "malformed-response":
						return &sdk.ElicitResult{Action: "accept"}, nil
					case "negative":
						return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": false}}, nil
					default:
						return &sdk.ElicitResult{Action: scenario}, nil
					}
				}})
				if version == "2026-07-28" && scenario == "transport-error" {
					// An MRTR client can stop locally before sending any response to
					// the server. It cannot trigger a server-side fallback in that case.
					_, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "orders", Arguments: browserOrderArgs()})
					require.ErrorContains(t, err, "native form unavailable")
					require.Zero(t, f.calls.Load())
					require.Zero(t, f.b.pending.Len())
					return
				}
				result := callBrowserOrder(t, cs, browserOrderArgs())
				require.Zero(t, f.calls.Load(), "native failure and fallback must not submit an order")
				data := approvalObservation(t, result)
				require.Equal(t, true, data["native_elicitation_attempted"])
				fallback := scenario == "transport-error" || scenario == "malformed-response"
				require.Equal(t, !fallback, data["native_elicitation_completed"])
				if fallback {
					require.Equal(t, "browser", data["approval_method_used"])
					require.Equal(t, "required", data["approval_outcome"])
					reason := "malformed_native_response"
					if scenario == "transport-error" {
						reason = "native_transport_error"
					}
					require.Equal(t, reason, data["fallback_reason"])
				} else {
					require.Equal(t, "native", data["approval_method_used"])
					outcome := "declined"
					if scenario == "cancel" {
						outcome = "cancelled"
					}
					require.Equal(t, outcome, data["approval_outcome"])
					require.Empty(t, data["fallback_reason"])
				}
				if scenario != "transport-error" && scenario != "malformed-response" {
					require.True(t, result.IsError)
					require.Zero(t, f.b.pending.Len(), "explicit user decisions must not create another review")
					return
				}
				id := browserApprovalID(t, result)
				csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
				require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}}, "").Code)
				require.EqualValues(t, 1, f.calls.Load())
				observed := approvalObservation(t, callBrowserOrder(t, cs, map[string]any{"mode": "approval_status", "approval_id": id}))
				require.Equal(t, "approved", observed["approval_outcome"])
				require.Equal(t, data["fallback_reason"], observed["fallback_reason"])
				require.Equal(t, true, observed["native_elicitation_attempted"])
				require.Equal(t, false, observed["native_elicitation_completed"])
			})
		}
	}
}

func TestGenericElicitationUsesBrowserAutomatically(t *testing.T) {
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(version, func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			cs, _ := f.connect(t, "approval-test", "Generic legacy client", version, &sdk.ClientOptions{
				Capabilities: &sdk.ClientCapabilities{Elicitation: &sdk.ElicitationCapabilities{}},
				ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
					t.Error("generic capabilities must not require the user to request browser fallback")
					return &sdk.ElicitResult{Action: "decline"}, nil
				},
			})
			result := callBrowserOrder(t, cs, browserOrderArgs())
			browserApprovalID(t, result)
			observation := approvalObservation(t, result)
			require.Equal(t, "browser", observation["approval_method_used"])
			require.Equal(t, "form_support_unspecified", observation["fallback_reason"])
			require.Equal(t, false, observation["native_elicitation_attempted"])
			require.Equal(t, false, observation["native_elicitation_completed"])
			require.Zero(t, f.calls.Load())
		})
	}
}

func TestBrowserApprovalTrustAndRevocation(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	form := url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, identity, form, "").Code)
	auto := callBrowserOrder(t, cs, browserOrderArgs())
	require.False(t, auto.IsError)
	observation := approvalObservation(t, auto)
	require.Equal(t, "remembered_permission", observation["approval_method_used"])
	require.Equal(t, "approved", observation["approval_outcome"])
	require.Equal(t, false, observation["native_elicitation_attempted"])
	require.Equal(t, false, observation["native_elicitation_completed"])
	require.EqualValues(t, 2, f.calls.Load())
	permissions := callBrowserOrder(t, cs, map[string]any{"mode": "permissions"})
	require.Contains(t, permissions.Content[0].(*sdk.TextContent).Text, `"future_orders_allowed":true`)
	other, _ := f.connect(t, "other-grant", "Pi", "2025-11-25")
	browserApprovalID(t, callBrowserOrder(t, other, browserOrderArgs()))
	require.EqualValues(t, 2, f.calls.Load(), "same display name cannot inherit permission")
	page := browserPage(t, f.b, "manage/approval-test", identity)
	if path := os.Getenv("KITE_APPROVAL_PREVIEW_PATH"); path != "" {
		require.NoError(t, os.WriteFile(path+".manage.html", page.Body.Bytes(), 0600))
		require.NoError(t, os.WriteFile(path+".confirmed.html", browserPage(t, f.b, id, identity).Body.Bytes(), 0600))
	}
	revoke := url.Values{"csrf": {pageCSRF(t, page)}, "decision": {"revoke"}}
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, "manage/approval-test", identity, revoke, "").Code)
	browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	require.EqualValues(t, 2, f.calls.Load())
	// A replayed management form can revoke but can never enable a permission.
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, "manage/approval-test", identity, revoke, "").Code)
}

func TestBrowserApprovalRejectsUntrustedRequests(t *testing.T) {
	for _, scenario := range []string{"csrf", "origin", "wrong-review", "decline", "expired", "logged-out", "changed-arguments", "changed-history", "changed-account"} {
		t.Run(scenario, func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
			args := browserOrderArgs()
			if scenario == "changed-history" {
				args = map[string]any{"mode": "modify", "order_id": "order-1", "order_type": "LIMIT", "price": 1400}
			}
			id := browserApprovalID(t, callBrowserOrder(t, cs, args))
			csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
			form := url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}
			origin := ""
			switch scenario {
			case "csrf":
				form.Set("csrf", "forged")
			case "origin":
				origin = "https://evil.example"
			case "wrong-review":
				otherID := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
				form.Set("csrf", pageCSRF(t, browserPage(t, f.b, otherID, identity)))
			case "decline":
				form.Set("decision", "decline")
			case "expired":
				f.b.pending.Get(id).expires = time.Now().Add(-time.Second)
			case "logged-out":
				_, err := f.manager.SessionManager().Terminate("approval-test")
				require.NoError(t, err)
			case "changed-account":
				require.NoError(t, f.manager.SessionManager().UpdateCredentials("approval-test", &kc.KiteCredentials{UserID: "other-account", AccessToken: "test-access", ExpiresAt: time.Now().Add(time.Hour)}))
				require.Equal(t, http.StatusForbidden, browserPage(t, f.b, id, identity).Code)
			case "changed-arguments":
				f.b.pending.Get(id).arguments = json.RawMessage(`{"mode":"place","exchange":"NSE","tradingsymbol":"INFY","transaction_type":"BUY","quantity":2,"product":"CNC","order_type":"LIMIT","price":1500}`)
			case "changed-history":
				f.b.pending.Get(id).snapshot = []byte(`{"order_id":"different"}`)
			}
			postBrowserDecision(f.b, id, identity, form, origin)
			require.Zero(t, f.calls.Load())
			require.Zero(t, f.b.permissions.Len())
		})
	}
}

func TestBrowserApprovalConcurrentPosts(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}}, "")
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, f.calls.Load())
}

func TestBrowserApprovalStatusIsConnectionBound(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	other, _ := f.connect(t, "other-grant", "Pi", "2025-11-25")
	require.True(t, callBrowserOrder(t, other, map[string]any{"mode": "approval_status", "approval_id": id}).IsError)
}

func TestExplicitBrowserApprovalAndExpiredPermission(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	nativeCalls := 0
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25", &sdk.ClientOptions{ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
		nativeCalls++
		return &sdk.ElicitResult{Action: "decline"}, nil
	}})
	args := browserOrderArgs()
	args["approval_method"] = "browser"
	id := browserApprovalID(t, callBrowserOrder(t, cs, args))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	require.Equal(t, http.StatusOK, postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}, "").Code)
	require.Zero(t, nativeCalls)
	require.EqualValues(t, 1, f.calls.Load())
	// A user explicitly asking for another browser review must receive it even
	// while an earlier remembered permission exists.
	browserApprovalID(t, callBrowserOrder(t, cs, args))
	require.EqualValues(t, 1, f.calls.Load())
	f.b.mu.Lock()
	permission := f.b.permissions.Get("approval-test")
	permission.expires = time.Now().Add(-time.Second)
	f.b.permissions.Set("approval-test", permission)
	f.b.mu.Unlock()
	// Native forms still work after remembered permission expires.
	result := callBrowserOrder(t, cs, browserOrderArgs())
	require.True(t, result.IsError)
	require.Equal(t, 1, nativeCalls)
	require.EqualValues(t, 1, f.calls.Load())
}

func TestBrowserApprovalFailedSubmissionNeverRetriesOrRemembers(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	f.failMutations.Store(true)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	form := url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}
	postBrowserDecision(f.b, id, identity, form, "")
	postBrowserDecision(f.b, id, identity, form, "")
	require.EqualValues(t, 1, f.calls.Load())
	require.Zero(t, f.b.permissions.Len())
	result := callBrowserOrder(t, cs, map[string]any{"mode": "approval_status", "approval_id": id})
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, `"status":"failed"`)
}

func TestRevocationDuringSubmissionCannotBeUndone(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}, "")
	args := browserOrderArgs()
	args["approval_method"] = "browser"
	id = browserApprovalID(t, callBrowserOrder(t, cs, args))
	csrf = pageCSRF(t, browserPage(t, f.b, id, identity))
	f.blockMutations.Store(true)
	done := make(chan struct{})
	go func() {
		defer close(done)
		postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}, "")
	}()
	select {
	case <-f.started:
	case <-time.After(3 * time.Second):
		t.Fatal("mutation did not start")
	}
	page := browserPage(t, f.b, "manage/approval-test", identity)
	postBrowserDecision(f.b, "manage/approval-test", identity, url.Values{"csrf": {pageCSRF(t, page)}, "decision": {"revoke"}}, "")
	close(f.release)
	<-done
	result := callBrowserOrder(t, cs, map[string]any{"mode": "permissions"})
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, `"future_orders_allowed":false`)
	require.EqualValues(t, 2, f.calls.Load())
}

func TestBrowserApprovalCompletedOutcomesDoNotBlockNewReviews(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "active-capacity", true: "completed-capacity"}[terminal], func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
			now := time.Now()
			for i := 0; i < maxPendingTradeApprovals; i++ {
				id := randomApprovalSecret()
				status := "approval_required"
				if terminal {
					status = "submitted"
				}
				f.b.pending.Set(id, &browserOrder{id: id, session: "other", status: status, expires: now.Add(time.Minute), retainUntil: now.Add(10 * time.Minute)})
			}
			result := callBrowserOrder(t, cs, browserOrderArgs())
			if terminal {
				browserApprovalID(t, result)
			} else {
				require.True(t, result.IsError)
				require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "busy")
			}
			require.Equal(t, maxPendingTradeApprovals, f.b.pending.Len())
			require.Zero(t, f.calls.Load())
		})
	}
}

func TestBrowserApprovalResultMakesPermissionOutcomeExplicit(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "submitted", true: "failed"}[fail], func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			f.failMutations.Store(fail)
			cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
			id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
			csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
			postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}, "")
			page := browserPage(t, f.b, id, identity)
			require.Equal(t, http.StatusOK, page.Code)
			if fail {
				require.Contains(t, page.Body.String(), "Future orders still require approval")
				require.NotContains(t, page.Body.String(), "Ask me before every order")
			} else {
				require.Contains(t, page.Body.String(), "Pi can place, modify, and cancel orders without asking")
				require.Contains(t, page.Body.String(), "Ask me before every order")
			}
		})
	}
}

func TestBrowserApprovalRejectsMalformedForms(t *testing.T) {
	for _, scenario := range []string{"missing-csrf", "duplicate-csrf", "duplicate-decision", "duplicate-remember", "invalid-remember", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
			id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
			csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
			form := url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}
			switch scenario {
			case "missing-csrf":
				form.Del("csrf")
			case "duplicate-csrf":
				form.Add("csrf", csrf)
			case "duplicate-decision":
				form.Add("decision", "decline")
			case "duplicate-remember":
				form.Add("remember", "yes")
			case "invalid-remember":
				form.Set("remember", "true")
			case "oversized":
				form.Set("padding", strings.Repeat("x", 8192))
			}
			response := postBrowserDecision(f.b, id, identity, form, "")
			require.GreaterOrEqual(t, response.Code, 400)
			require.Zero(t, f.calls.Load())
			require.Zero(t, f.b.permissions.Len())
		})
	}
}

func TestBrowserApprovalArgumentsCannotGrantPermission(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
	args := browserOrderArgs()
	for _, key := range []string{"approved", "confirm", "remember", "future_orders_allowed"} {
		args[key] = true
	}
	browserApprovalID(t, callBrowserOrder(t, cs, args))
	require.Zero(t, f.calls.Load())
	require.Zero(t, f.b.permissions.Len())
}

func TestBrowserApprovalPermissionAndStatusRequireSameClient(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	owner, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, owner, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}, "")
	other, _ := f.connect(t, "approval-test", "Pi", "2025-11-25")
	require.True(t, callBrowserOrder(t, other, map[string]any{"mode": "approval_status", "approval_id": id}).IsError)
	browserApprovalID(t, callBrowserOrder(t, other, browserOrderArgs()))
	require.EqualValues(t, 1, f.calls.Load(), "same grant and display name must not bypass client binding")
	require.False(t, callBrowserOrder(t, owner, browserOrderArgs()).IsError)
	require.EqualValues(t, 2, f.calls.Load())
}

func TestBrowserApprovalDeadlineCancelsUpstreamWork(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, map[string]any{"mode": "cancel", "order_id": "order-1"}))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	f.b.mu.Lock()
	f.b.pending.Get(id).expires = time.Now().Add(500 * time.Millisecond)
	f.b.mu.Unlock()
	f.blockHistory.Store(true)
	done := make(chan struct{})
	go func() {
		defer close(done)
		postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}, "remember": {"yes"}}, "")
	}()
	for _, signal := range []<-chan struct{}{f.historyStarted, done, f.historyStopped} {
		select {
		case <-signal:
		case <-time.After(3 * time.Second):
			t.Fatal("approval expiry did not cancel upstream work")
		}
	}
	require.Zero(t, f.calls.Load())
	require.Zero(t, f.b.permissions.Len())
}

func TestBrowserApprovalRetainsNotesAndEscapesUntrustedText(t *testing.T) {
	for _, orderType := range []string{"MARKET", "SL-M", "LIMIT"} {
		t.Run(orderType, func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			cs, identity := f.connect(t, "approval-test", `<script>alert(1)</script>`, "2025-11-25")
			args := browserOrderArgs()
			args["order_type"] = orderType
			args["tradingsymbol"] = "INFY\nQuantity: 500\u202e"
			id := browserApprovalID(t, callBrowserOrder(t, cs, args))
			page := browserPage(t, f.b, id, identity)
			body := page.Body.String()
			require.Contains(t, body, "Other order settings use Kite&#39;s defaults.")
			require.Equal(t, orderType != "LIMIT", strings.Contains(body, "The market decides the execution price."))
			require.NotContains(t, body, "<script>")
			require.Contains(t, body, "&lt;script&gt;")
			require.NotContains(t, body, "\u202e")
			require.NotContains(t, body, "<dd>500")
			require.Contains(t, page.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
			require.Equal(t, "nosniff", page.Header().Get("X-Content-Type-Options"))
			require.Zero(t, f.calls.Load())
		})
	}
}

func approvalObservation(t *testing.T, result *sdk.CallToolResult) map[string]any {
	t.Helper()
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].(*sdk.TextContent).Text), &data))
	encoded, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var structured map[string]any
	require.NoError(t, json.Unmarshal(encoded, &structured))
	require.Equal(t, data, structured, "text and structured observations must agree")
	return data
}

func TestBrowserApprovalSaveForLater(t *testing.T) {
	for _, scenario := range []string{"once", "changed", "other-client", "expired", "revoked", "failed", "browser-review"} {
		t.Run(scenario, func(t *testing.T) {
			f := newBrowserApprovalFixture(t)
			cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
			id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
			page := browserPage(t, f.b, id, identity)
			require.Contains(t, page.Body.String(), "Approve for later. Do not place now")
			csrf := pageCSRF(t, page)
			form := url.Values{"csrf": {csrf}, "decision": {"save"}}
			outcome := postBrowserDecision(f.b, id, identity, form, "")
			require.Equal(t, http.StatusOK, outcome.Code)
			require.Contains(t, outcome.Body.String(), "Approval saved. Order not placed")
			require.Zero(t, f.calls.Load())
			// Replaying either decision on this completed form must not submit.
			postBrowserDecision(f.b, id, identity, form, "")
			postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"approve"}}, "")
			require.Zero(t, f.calls.Load())
			status := callBrowserOrder(t, cs, map[string]any{"mode": "approval_status", "approval_id": id})
			require.Contains(t, status.Content[0].(*sdk.TextContent).Text, `"status":"approved_for_later"`)
			require.Contains(t, status.Content[0].(*sdk.TextContent).Text, `"future_orders_allowed":false`)
			permission := callBrowserOrder(t, cs, map[string]any{"mode": "permissions"})
			require.Contains(t, permission.Content[0].(*sdk.TextContent).Text, `"saved_order_approvals":1`)
			args := browserOrderArgs()
			switch scenario {
			case "changed":
				args["quantity"] = 2
			case "other-client":
				cs, _ = f.connect(t, "approval-test", "Pi", "2025-11-25")
			case "expired":
				f.b.mu.Lock()
				f.b.pending.Get(id).expires = time.Now().Add(-time.Second)
				f.b.mu.Unlock()
			case "revoked":
				path := "manage/approval-test"
				manage := browserPage(t, f.b, path, identity)
				require.Contains(t, manage.Body.String(), "saved order approvals")
				postBrowserDecision(f.b, path, identity, url.Values{"csrf": {pageCSRF(t, manage)}, "decision": {"revoke"}}, "")
			case "failed":
				f.failMutations.Store(true)
			case "browser-review":
				args["approval_method"] = "browser"
			}
			result := callBrowserOrder(t, cs, args)
			if scenario == "once" || scenario == "failed" {
				require.EqualValues(t, 1, f.calls.Load())
				require.Equal(t, scenario == "failed", result.IsError)
				// Permission is consumed even if the submission fails.
				browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
				require.EqualValues(t, 1, f.calls.Load())
			} else {
				browserApprovalID(t, result)
				require.Zero(t, f.calls.Load())
			}
		})
	}
}

func TestBrowserApprovalRejectsSaveWithBroadPermission(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	page := postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"save"}, "remember": {"yes"}}, "")
	require.Equal(t, http.StatusBadRequest, page.Code)
	require.Zero(t, f.calls.Load())
	require.Zero(t, f.b.permissions.Len())
}

func TestBrowserApprovalSavedOrderConcurrentUse(t *testing.T) {
	f := newBrowserApprovalFixture(t)
	cs, identity := f.connect(t, "approval-test", "Pi", "2025-11-25")
	id := browserApprovalID(t, callBrowserOrder(t, cs, browserOrderArgs()))
	csrf := pageCSRF(t, browserPage(t, f.b, id, identity))
	postBrowserDecision(f.b, id, identity, url.Values{"csrf": {csrf}, "decision": {"save"}}, "")
	f.blockMutations.Store(true)
	done := make(chan *sdk.CallToolResult, 1)
	go func() {
		result, _ := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "orders", Arguments: browserOrderArgs()})
		done <- result
	}()
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("saved approval did not start submission")
	}
	// A second request cannot reuse approval while the first is in flight.
	result, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "orders", Arguments: browserOrderArgs()})
	close(f.release)
	require.NoError(t, err)
	browserApprovalID(t, result)
	require.NotNil(t, <-done)
	require.EqualValues(t, 1, f.calls.Load())
}
