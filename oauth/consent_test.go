package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

func consentTestHandlers(t *testing.T, baseURL string) *Handlers {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := kc.New(kc.Config{APIKey: "test-key", APISecret: "test-secret", Logger: logger, Instruments: &instruments.Manager{}, KiteBaseURI: baseURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.SessionManager().StopCleanupRoutine)
	return NewHandlers(New(Config{Issuer: "https://mcp.example", JWTSecret: []byte(strings.Repeat("s", 32)), SessionExpiry: manager.SessionExpiry}), manager, logger)
}

func consentPage(t *testing.T, h *Handlers) (url.Values, *http.Cookie, string) {
	t.Helper()
	redirect := "https://client.example/callback"
	id, err := h.server.RegisterClient([]string{redirect}, "Example <Client>")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(strings.Repeat("v", 43)))
	q := url.Values{"client_id": {id}, "redirect_uri": {redirect}, "response_type": {"code"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}, "state": {"client-state"}}
	w := httptest.NewRecorder()
	h.HandleAuthorize(w, httptest.NewRequest("GET", "/authorize?"+q.Encode(), nil))
	if w.Code != 200 {
		t.Fatalf("consent GET: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Example &lt;Client&gt;", redirect, "Kite Connect API", "balances", "trading"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing consent detail %q", want)
		}
	}
	if strings.Contains(body, "kite.zerodha.com") {
		t.Fatal("upstream login exposed before consent")
	}
	if w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing consent security headers")
	}
	form := url.Values{"decision": {"approve"}}
	for _, name := range []string{"flow", "csrf"} {
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(body)
		if len(match) != 2 {
			t.Fatalf("missing %s", name)
		}
		form.Set(name, html.UnescapeString(match[1]))
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected consent cookie only: %v", cookies)
	}
	c := cookies[0]
	if !strings.HasPrefix(c.Name, "__Host-") || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatal("insecure consent cookie")
	}
	return form, c, id
}

func postConsent(h *Handlers, form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/authorize", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.HandleAuthorize(w, r)
	return w
}

func TestConsentRejectsMissingTamperedOrExpiredBrowserProof(t *testing.T) {
	for _, kind := range []string{"missing-cookie", "wrong-cookie", "wrong-csrf", "expired"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := consentTestHandlers(t, "")
				form, c, _ := consentPage(t, h)
				switch kind {
				case "missing-cookie":
					c = nil
				case "wrong-cookie":
					c.Value = "other"
				case "wrong-csrf":
					form.Set("csrf", "other")
				case "expired":
					time.Sleep(11 * time.Minute)
				}
				w := postConsent(h, form, c)
				if w.Code < 400 || w.Header().Get("Location") != "" {
					t.Fatalf("accepted %s: %d", kind, w.Code)
				}
				for _, cookie := range w.Result().Cookies() {
					if strings.Contains(cookie.Name, "-state-") {
						t.Fatal("state cookie set without consent")
					}
				}
			})
		})
	}
}

func TestConsentCallbackBrowserBindingAndReplay(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"success","data":{"user_id":"test-user","access_token":"access","refresh_token":"refresh"}}`)
	}))
	defer upstream.Close()
	h := consentTestHandlers(t, upstream.URL)
	form, c, id := consentPage(t, h)
	flow := form.Get("flow")
	signed, err := h.kcManager.SessionSigner().SignRedirectParams(flow)
	if err != nil {
		t.Fatal(err)
	}
	callbackURL := "/callback?request_token=test&" + signed
	// A signed login URL alone cannot authorize a callback.
	before := httptest.NewRecorder()
	h.HandleCallback(before, httptest.NewRequest("GET", callbackURL, nil))
	if before.Code != 400 || calls.Load() != 0 {
		t.Fatal("callback bypassed consent")
	}
	approved := postConsent(h, form, c)
	if approved.Code != 303 {
		t.Fatalf("approve: %d %s", approved.Code, approved.Body.String())
	}
	login, err := url.Parse(approved.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	callbackURL = "/callback?request_token=test&" + login.Query().Get("redirect_params")
	var state *http.Cookie
	for _, cookie := range approved.Result().Cookies() {
		if strings.Contains(cookie.Name, "-state-") {
			state = cookie
		}
	}
	if state == nil || !state.Secure || !state.HttpOnly {
		t.Fatal("missing secure callback binding")
	}
	// The attacker's approved link cannot be used in a different browser.
	foreign := httptest.NewRecorder()
	h.HandleCallback(foreign, httptest.NewRequest("GET", callbackURL, nil))
	if foreign.Code != 400 || calls.Load() != 0 {
		t.Fatal("foreign browser callback accepted")
	}
	// The state query must match as well as the browser cookie.
	tamperedURL, _ := url.Parse(callbackURL)
	tamperedQuery := tamperedURL.Query()
	tamperedQuery.Set("state", "other")
	tamperedURL.RawQuery = tamperedQuery.Encode()
	tamperedRequest := httptest.NewRequest("GET", tamperedURL.String(), nil)
	tamperedRequest.AddCookie(state)
	tampered := httptest.NewRecorder()
	h.HandleCallback(tampered, tamperedRequest)
	if tampered.Code != 400 || calls.Load() != 0 {
		t.Fatal("tampered callback state accepted")
	}
	// Concurrent callbacks exchange the upstream credential only once.
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", callbackURL, nil)
			r.AddCookie(state)
			w := httptest.NewRecorder()
			h.HandleCallback(w, r)
			responses <- w
		}()
	}
	wg.Wait()
	close(responses)
	var location string
	for w := range responses {
		if w.Code == 302 {
			location = w.Header().Get("Location")
		}
	}
	if location == "" || calls.Load() != 1 {
		t.Fatalf("callback replay protection: calls=%d location=%q", calls.Load(), location)
	}
	u, _ := url.Parse(location)
	if u.Query().Get("state") != "client-state" {
		t.Fatal("client state lost")
	}
	token, err := h.server.ExchangeCode(&TokenRequest{Code: u.Query().Get("code"), ClientID: id, RedirectURI: "https://client.example/callback", CodeVerifier: strings.Repeat("v", 43)})
	if err != nil || token == "" {
		t.Fatalf("approved exchange failed: %v", err)
	}
	if retry := postConsent(h, form, c); retry.Code < 400 {
		t.Fatal("consent replay accepted")
	}
}

func TestConsentCannotChangeRegisteredRecipient(t *testing.T) {
	h := consentTestHandlers(t, "")
	form, c, _ := consentPage(t, h)
	form.Set("redirect_uri", "https://attacker.example/callback")
	form.Set("client_id", "attacker")
	w := postConsent(h, form, c)
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	s, err := h.kcManager.SessionManager().Get(form.Get("flow"))
	if err != nil {
		t.Fatal(err)
	}
	p := s.OAuthData.(*pendingAuth)
	if p.RedirectURI != "https://client.example/callback" {
		t.Fatal("recipient changed")
	}
	// No authorization code exists before upstream login.
	h.server.mu.RLock()
	defer h.server.mu.RUnlock()
	if len(h.server.authCodes) != 0 {
		t.Fatal("premature authorization code")
	}
}

func TestRegistrationNameSurvivesHandler(t *testing.T) {
	h := consentTestHandlers(t, "")
	w := httptest.NewRecorder()
	h.HandleRegister(w, httptest.NewRequest("POST", "/register", strings.NewReader(`{"client_name":"Named Client","redirect_uris":["https://client.example/callback"]}`)))
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	name, err := h.server.ClientName(result["client_id"].(string))
	if err != nil || name != "Named Client" {
		t.Fatalf("lost name: %q %v", name, err)
	}
}

func TestHandlersRejectForeignResources(t *testing.T) {
	h := consentTestHandlers(t, "")
	id, err := h.server.RegisterClient([]string{"https://client.example/callback"})
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"client_id": {id}, "redirect_uri": {"https://client.example/callback"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}, "resource": {"https://foreign.example/mcp"}}
	w := httptest.NewRecorder()
	h.HandleAuthorize(w, httptest.NewRequest("GET", "/authorize?"+q.Encode(), nil))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_target") || len(w.Result().Cookies()) != 0 {
		t.Fatal("foreign authorization resource accepted")
	}
	q.Set("grant_type", "authorization_code")
	q.Set("code", "unused")
	q.Set("code_verifier", "verifier")
	r := httptest.NewRequest("POST", "/token", strings.NewReader(q.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.HandleToken(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_target") {
		t.Fatal("foreign token resource accepted")
	}
}

func TestDeclinedConsentReturnsOAuthError(t *testing.T) {
	h := consentTestHandlers(t, "")
	form, c, _ := consentPage(t, h)
	form.Set("decision", "deny")
	w := postConsent(h, form, c)
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != 303 || u.Host != "client.example" || u.Query().Get("error") != "access_denied" || u.Query().Get("state") != "client-state" {
		t.Fatalf("invalid denial response: %d %s", w.Code, w.Header().Get("Location"))
	}
	for _, cookie := range w.Result().Cookies() {
		if strings.Contains(cookie.Name, "-state-") {
			t.Fatal("callback state set after denial")
		}
	}
	if repeat := postConsent(h, form, c); repeat.Code < 400 {
		t.Fatal("denied flow reusable")
	}
}
