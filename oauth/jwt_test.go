package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

func TestValidateRedirectURI(t *testing.T) {
	srv := New(Config{})

	valid := []string{
		"http://localhost:3000/callback",
		"http://127.0.0.1:8080/callback",
		"https://localhost/callback",
		"http://[::1]:8080/callback",
		"https://new-client.example/callback",
		"https://claude.ai/api/mcp/auth_callback",
		"https://chatgpt.com/connector_platform_oauth_redirect",
	}
	for _, uri := range valid {
		if err := srv.ValidateRedirectURI(uri); err != nil {
			t.Fatalf("expected %q to be allowed, got error: %v", uri, err)
		}
	}

	invalid := []string{
		"http://remote.example/callback", "javascript:alert(1)", "/callback",
		"https://user:pass@example.com/callback", "https://example.com/callback#fragment",
		"http://localhost.evil.example/callback", "https:///callback",
	}
	for _, uri := range invalid {
		if err := srv.ValidateRedirectURI(uri); err == nil {
			t.Fatalf("expected %q to be rejected", uri)
		}
	}
}

func TestMiddlewareBindsSDKUserIdentity(t *testing.T) {
	srv := New(Config{Issuer: "https://mcp.example", JWTSecret: []byte("01234567890123456789012345678901")})
	token, err := srv.generateAccessToken("kite-user", "opaque-grant")
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := auth.TokenInfoFromContext(r.Context())
		if info == nil || info.UserID != "kite-user" {
			t.Fatalf("SDK user identity was not bound: %#v", info)
		}
		if got := r.Header.Get("X-Kite-Session-Id"); got != "opaque-grant" {
			t.Fatalf("Kite session header = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("middleware response = %d: %s", resp.Code, resp.Body.String())
	}
}

func TestClientIPOnlyTrustsConfiguredProxy(t *testing.T) {
	srv := New(Config{TrustedProxyCIDRs: []string{"10.0.0.0/8"}})
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.2.3.4:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.10, 10.1.1.1")
	if got := srv.ClientIP(req); got != "198.51.100.10" {
		t.Fatalf("trusted proxy client IP = %q, want 198.51.100.10", got)
	}

	req.RemoteAddr = "198.51.100.20:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	if got := srv.ClientIP(req); got != "198.51.100.20" {
		t.Fatalf("untrusted peer client IP = %q, want direct peer", got)
	}
}

func TestDefaultTokenLifetimeIsSevenDays(t *testing.T) {
	srv := New(Config{Issuer: "https://mcp.example", JWTSecret: []byte("01234567890123456789012345678901")})
	token, err := srv.generateAccessToken("user", "grant")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := srv.ValidateToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if got := claims.ExpiresAt.Sub(claims.IssuedAt.Time); got != 7*24*time.Hour {
		t.Fatalf("token lifetime = %v, want seven days", got)
	}
}

func TestDelayedExchangeUsesGrantDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deadline := time.Now().Add(7 * 24 * time.Hour)
		srv := New(Config{Issuer: "https://mcp.example", JWTSecret: []byte("01234567890123456789012345678901"), SessionExpiry: func(id string) (time.Time, error) {
			if id != "grant" || !time.Now().Before(deadline) {
				return time.Time{}, errors.New("expired grant")
			}
			return deadline, nil
		}})
		verifier := "test-pkce-verifier-of-at-least-forty-three-characters"
		hash := sha256.Sum256([]byte(verifier))
		code, err := srv.CreateAuthCode("client", "http://localhost/callback", "user", "grant", base64.RawURLEncoding.EncodeToString(hash[:]))
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(9 * time.Minute)
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {"client"}, "redirect_uri": {"http://localhost/callback"}, "code_verifier": {verifier}}
		request := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		handler := NewHandlers(srv, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		handler.HandleToken(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("token exchange returned %d", recorder.Code)
		}
		var response struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		claims, err := srv.ValidateToken(response.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		if !claims.ExpiresAt.Equal(deadline) {
			t.Fatal("JWT outlives grant")
		}
		if response.ExpiresIn != int(time.Until(deadline).Seconds()) {
			t.Fatalf("expires_in=%d does not match remaining lifetime", response.ExpiresIn)
		}
		time.Sleep(time.Until(deadline))
		if _, err := srv.ValidateToken(response.AccessToken); err == nil {
			t.Fatal("JWT still valid at grant expiry")
		}
	})
}

func TestMiddlewareChallengesEndedGrant(t *testing.T) {
	for _, reason := range []string{"expired", "logged-out", "missing"} {
		t.Run(reason, func(t *testing.T) {
			ended := false
			srv := New(Config{Issuer: "https://mcp.example", JWTSecret: []byte("01234567890123456789012345678901"), SessionExpiry: func(string) (time.Time, error) {
				if ended {
					return time.Time{}, errors.New(reason)
				}
				return time.Now().Add(time.Hour), nil
			}})
			token, err := srv.generateAccessToken("user", "grant")
			if err != nil {
				t.Fatal(err)
			}
			ended = true
			called := false
			handler := srv.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if called || recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("ended grant did not produce a 401 challenge")
			}
			if _, err := srv.generateAccessToken("user", "grant"); err == nil {
				t.Fatal("issued a JWT for an ended grant")
			}
		})
	}
}

func TestOpenRegistrationRateLimits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := NewHandlers(New(Config{JWTSecret: []byte("01234567890123456789012345678901")}), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		register := func(ip string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"redirect_uris":["https://new-client.example/callback"]}`))
			req.RemoteAddr = ip + ":1234"
			req.Header.Set("X-Forwarded-For", "203.0.113.99")
			resp := httptest.NewRecorder()
			h.HandleRegister(resp, req)
			return resp
		}
		for i := 0; i < 10; i++ {
			if resp := register("198.51.100.1"); resp.Code != http.StatusCreated {
				t.Fatalf("registration %d: %d %s", i, resp.Code, resp.Body.String())
			}
		}
		if resp := register("198.51.100.1"); resp.Code != http.StatusTooManyRequests || resp.Header().Get("Retry-After") != "60" {
			t.Fatalf("minute limit: %d %v", resp.Code, resp.Header())
		}
		if resp := register("198.51.100.2"); resp.Code != http.StatusCreated {
			t.Fatalf("independent IP: %d", resp.Code)
		}
		time.Sleep(time.Minute)
		if resp := register("198.51.100.1"); resp.Code != http.StatusCreated {
			t.Fatalf("minute recovery: %d", resp.Code)
		}
		for i := 0; i < 88; i++ {
			if i%9 == 0 {
				time.Sleep(time.Minute)
			}
			if resp := register("198.51.100.1"); resp.Code != http.StatusCreated {
				t.Fatalf("hour request %d: %d", i, resp.Code)
			}
		}
		if resp := register("198.51.100.1"); resp.Code != http.StatusTooManyRequests || resp.Header().Get("Retry-After") != "3600" {
			t.Fatalf("hour limit: %d %v", resp.Code, resp.Header())
		}
		time.Sleep(time.Hour)
		if resp := register("198.51.100.1"); resp.Code != http.StatusCreated {
			t.Fatalf("hour recovery: %d", resp.Code)
		}
	})
}

func TestRateLimiterRemovesExpiredPeers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewRateLimiter(1, time.Minute)
		limiter.Allow("old-peer")
		time.Sleep(time.Minute)
		if !limiter.Allow("new-peer") {
			t.Fatal("new peer rejected")
		}
		if _, retained := limiter.requests["old-peer"]; retained {
			t.Fatal("expired peer retained")
		}
	})
}

func TestAuthorizeRequiresRegisteredCallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := New(Config{JWTSecret: []byte("01234567890123456789012345678901")})
		callback := "https://client.example/callback"
		id, err := srv.RegisterClient([]string{callback})
		if err != nil {
			t.Fatal(err)
		}
		h := consentTestHandlers(t, "")
		h.server = srv
		for _, tc := range []struct{ name, clientID, redirect string }{
			{"unknown client", "unregistered", callback},
			{"attacker callback", id, "https://attacker.example/callback"},
			{"different path", id, callback + "/other"},
			{"different query", id, callback + "?extra=1"},
		} {
			query := url.Values{"client_id": {tc.clientID}, "redirect_uri": {tc.redirect}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
			req := httptest.NewRequest(http.MethodGet, "/authorize?"+query.Encode(), nil)
			resp := httptest.NewRecorder()
			h.HandleAuthorize(resp, req)
			if resp.Code != http.StatusBadRequest || resp.Header().Get("Location") != "" {
				t.Fatalf("%s: unauthorized callback accepted: %d %v", tc.name, resp.Code, resp.Header())
			}
		}
		time.Sleep(48 * time.Hour)
		if err := srv.ValidateClientRedirect(id, callback); err != nil {
			t.Fatalf("registration expired: %v", err)
		}
	})
}

func TestRateLimiterEvictsLeastRecentlyUsedPeer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewRateLimiter(1, time.Hour)
		for i := 0; i < maxRateLimitPeers; i++ {
			if !limiter.Allow(fmt.Sprintf("peer-%d", i)) {
				t.Fatal("peer rejected before capacity")
			}
		}
		// Refresh the first peer, including when the request is throttled.
		if limiter.Allow("peer-0") {
			t.Fatal("existing limit was reset")
		}
		if !limiter.Allow("new-peer") {
			t.Fatal("new peer rejected at capacity")
		}
		if len(limiter.requests) != maxRateLimitPeers || limiter.peers.Len() != maxRateLimitPeers {
			t.Fatal("limiter exceeded capacity")
		}
		if _, retained := limiter.requests["peer-1"]; retained {
			t.Fatal("least recently used peer retained")
		}
		if limiter.Allow("peer-0") {
			t.Fatal("active peer lost its rate limit")
		}
	})
}

func TestRateLimiterGroupsIPv6Addresses(t *testing.T) {
	limiter := NewRateLimiter(1, time.Hour)
	if !limiter.Allow("2001:db8:1:2::1") {
		t.Fatal("first IPv6 request rejected")
	}
	if limiter.Allow("2001:db8:1:2::abcd") {
		t.Fatal("rotating IPv6 address bypassed limit")
	}
	if !limiter.Allow("2001:db8:1:3::1") {
		t.Fatal("independent IPv6 prefix rejected")
	}
	if !limiter.Allow("::ffff:192.0.2.1") {
		t.Fatal("IPv4-mapped address rejected")
	}
	if limiter.Allow("192.0.2.1") {
		t.Fatal("IPv4-mapped address bypassed IPv4 limit")
	}
}

func TestRegisteredClientCanAuthorize(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := kc.New(kc.Config{APIKey: "test-key", APISecret: "test-secret", Logger: logger, Instruments: &instruments.Manager{}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.SessionManager().StopCleanupRoutine()
	srv := New(Config{JWTSecret: []byte("01234567890123456789012345678901")})
	h := NewHandlers(srv, manager, logger)
	registration := httptest.NewRecorder()
	h.HandleRegister(registration, httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"redirect_uris":["https://new-client.example/callback"]}`)))
	if registration.Code != http.StatusCreated {
		t.Fatalf("registration: %d", registration.Code)
	}
	var client struct {
		ID string `json:"client_id"`
	}
	if err := json.Unmarshal(registration.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}
	query := url.Values{"client_id": {client.ID}, "redirect_uri": {"https://new-client.example/callback"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
	resp := httptest.NewRecorder()
	h.HandleAuthorize(resp, httptest.NewRequest(http.MethodGet, "/authorize?"+query.Encode(), nil))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "Approve and continue") {
		t.Fatalf("registered authorization: %d %s", resp.Code, resp.Body.String())
	}
}

func TestClientRegistrationSurvivesRestartAndTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := Config{Issuer: "https://mcp.example", JWTSecret: []byte("01234567890123456789012345678901")}
		original := New(cfg)
		callbacks := []string{"https://client.example/callback", "http://[::1]:8080/callback"}
		id, err := original.RegisterClient(callbacks)
		if err != nil {
			t.Fatal(err)
		}
		// Each new server has no registration metadata from the issuing instance.
		for _, instance := range []*Server{New(cfg), New(cfg)} {
			time.Sleep(30 * 24 * time.Hour)
			for _, callback := range callbacks {
				if err := instance.ValidateClientRedirect(id, callback); err != nil {
					t.Fatalf("registration lost across instances or time: %v", err)
				}
			}
			if err := instance.ValidateClientRedirect(id, "https://attacker.example/callback"); err == nil {
				t.Fatal("callback binding lost across instances")
			}
		}
		secondID, err := original.RegisterClient(callbacks)
		if err != nil {
			t.Fatal(err)
		}
		if id == secondID {
			t.Fatal("registrations must have unique IDs")
		}
	})
}

func TestClientRegistrationRejectsTamperingAndWrongSigningContext(t *testing.T) {
	cfg := Config{Issuer: "https://mcp.example", JWTSecret: []byte("01234567890123456789012345678901")}
	srv := New(cfg)
	callback := "https://client.example/callback"
	id, err := srv.RegisterClient([]string{callback})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(id, ".")
	changedPayload, err := json.Marshal(clientRegistration{Nonce: "attacker", RedirectHashes: []string{redirectHash("https://attacker.example/callback")}})
	if err != nil {
		t.Fatal(err)
	}
	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(changedPayload) + "." + parts[2]
	for _, bad := range []string{forged, id[:len(id)-1], "dcr2." + parts[1] + "." + parts[2], strings.Repeat("x", maxClientIDBytes+1)} {
		if err := srv.ValidateClientRedirect(bad, "https://attacker.example/callback"); err == nil {
			t.Fatal("forged registration accepted")
		}
	}
	for _, other := range []Config{
		{Issuer: cfg.Issuer, JWTSecret: []byte("different-secret-01234567890123456789")},
		{Issuer: "https://other.example", JWTSecret: cfg.JWTSecret},
		{Issuer: cfg.Issuer},
	} {
		if err := New(other).ValidateClientRedirect(id, callback); err == nil {
			t.Fatal("registration accepted with wrong signing context")
		}
	}
	accessToken, err := srv.generateAccessToken("user", "grant")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.ValidateClientRedirect(accessToken, callback); err == nil {
		t.Fatal("access token accepted as client ID")
	}
	if _, err := srv.ValidateToken(id); err == nil {
		t.Fatal("client ID accepted as access token")
	}
}

func TestClientRegistrationMaximumMetadata(t *testing.T) {
	srv := New(Config{JWTSecret: []byte("01234567890123456789012345678901")})
	callbacks := make([]string, maxDCRRedirectURIs)
	for i := range callbacks {
		prefix := fmt.Sprintf("https://client.example/%d/", i)
		callbacks[i] = prefix + strings.Repeat("x", maxDCRURIBytes-len(prefix))
	}
	id, err := srv.RegisterClient(callbacks)
	if err != nil {
		t.Fatal(err)
	}
	if len(id) > maxClientIDBytes {
		t.Fatalf("issued client ID exceeds validation limit: %d", len(id))
	}
	for _, callback := range callbacks {
		if err := srv.ValidateClientRedirect(id, callback); err != nil {
			t.Fatalf("maximum registration rejected: %v", err)
		}
	}
	if _, err := New(Config{}).RegisterClient(callbacks); err == nil {
		t.Fatal("registration signed without a secret")
	}
}
