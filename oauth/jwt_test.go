package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

func TestValidateRedirectURI_DefaultLocalhost(t *testing.T) {
	srv := New(Config{})

	valid := []string{
		"http://localhost:3000/callback",
		"http://127.0.0.1:8080/callback",
		"https://localhost/callback",
	}
	for _, uri := range valid {
		if err := srv.ValidateRedirectURI(uri); err != nil {
			t.Fatalf("expected %q to be allowed, got error: %v", uri, err)
		}
	}

	invalid := []string{
		"https://claude.ai/api/mcp/auth_callback",
		"https://chatgpt.com/connector_platform_oauth_redirect",
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

func TestRegisteredClientsAreBoundedAndExpire(t *testing.T) {
	srv := New(Config{MaxClients: 2, ClientTTL: time.Nanosecond})
	srv.RegisterClient([]string{"http://localhost:1/callback"})
	time.Sleep(time.Millisecond)
	srv.RegisterClient([]string{"http://localhost:2/callback"})

	srv.mu.RLock()
	count := len(srv.clients)
	srv.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expired clients retained: got %d, want 1", count)
	}

	srv = New(Config{MaxClients: 2})
	srv.RegisterClient([]string{"http://localhost:1/callback"})
	srv.RegisterClient([]string{"http://localhost:2/callback"})
	srv.RegisterClient([]string{"http://localhost:3/callback"})
	srv.mu.RLock()
	count = len(srv.clients)
	srv.mu.RUnlock()
	if count != 2 {
		t.Fatalf("client store exceeded cap: got %d, want 2", count)
	}
}

func TestValidateRedirectURI_ExplicitHostedCallbacks(t *testing.T) {
	srv := New(Config{AllowedRedirectPatterns: []string{
		"localhost",
		"https://claude.ai/api/mcp/auth_callback",
		"https://claude.com/api/mcp/auth_callback",
		"https://chatgpt.com/connector_platform_oauth_redirect",
	}})

	valid := []string{
		"http://localhost:3000/callback",
		"https://claude.ai/api/mcp/auth_callback",
		"https://claude.com/api/mcp/auth_callback",
		"https://chatgpt.com/connector_platform_oauth_redirect",
	}
	for _, uri := range valid {
		if err := srv.ValidateRedirectURI(uri); err != nil {
			t.Fatalf("expected %q to be allowed, got error: %v", uri, err)
		}
	}

	invalid := []string{
		"https://claude.ai/other-path",
		"https://chatgpt.com/other-callback",
		"https://evil.example.com/callback",
	}
	for _, uri := range invalid {
		if err := srv.ValidateRedirectURI(uri); err == nil {
			t.Fatalf("expected %q to be rejected", uri)
		}
	}
}

func TestValidateRedirectURI_RejectsHostnameOnlyPattern(t *testing.T) {
	srv := New(Config{AllowedRedirectPatterns: []string{"claude.ai"}})

	if err := srv.ValidateRedirectURI("https://claude.ai/api/mcp/auth_callback"); err == nil {
		t.Fatalf("expected hostname-only pattern to be rejected")
	}
}

func TestValidateRedirectURI_PrefixHostedCallbacks(t *testing.T) {
	srv := New(Config{AllowedRedirectPatterns: []string{
		"prefix:https://chatgpt.com/connector/oauth/",
	}})

	valid := []string{
		"https://chatgpt.com/connector/oauth/cb_123",
		"https://chatgpt.com/connector/oauth/abc-def",
	}
	for _, uri := range valid {
		if err := srv.ValidateRedirectURI(uri); err != nil {
			t.Fatalf("expected %q to be allowed, got error: %v", uri, err)
		}
	}

	invalid := []string{
		"https://chatgpt.com/connector_platform_oauth_redirect",
		"https://chatgpt.com/connector/other/cb_123",
		"https://evil.example.com/connector/oauth/cb_123",
	}
	for _, uri := range invalid {
		if err := srv.ValidateRedirectURI(uri); err == nil {
			t.Fatalf("expected %q to be rejected", uri)
		}
	}
}
