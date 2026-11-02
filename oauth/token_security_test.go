package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zerodha/kite-mcp-server/kc"
)

func TestTokenResponsesCannotBeCached(t *testing.T) {
	for _, scenario := range []string{"success", "invalid-request", "invalid-grant", "method"} {
		t.Run(scenario, func(t *testing.T) {
			s := New(Config{Issuer: "https://mcp.example", JWTSecret: []byte(strings.Repeat("s", 32))})
			verifier := strings.Repeat("v", 43)
			hash := sha256.Sum256([]byte(verifier))
			code, err := s.CreateAuthCode("client", "https://client.example/cb", "user", "session", base64.RawURLEncoding.EncodeToString(hash[:]))
			if err != nil {
				t.Fatal(err)
			}
			form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "client_id": {"client"}, "redirect_uri": {"https://client.example/cb"}}
			method := http.MethodPost
			switch scenario {
			case "invalid-request":
				form.Del("code")
			case "invalid-grant":
				form.Set("code", "unknown")
			case "method":
				method = http.MethodGet
			}
			r := httptest.NewRequest(method, "/token", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			h := NewHandlers(s, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
			h.HandleToken(w, r)
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" {
				t.Fatal("token response can be cached")
			}
			if scenario == "success" && w.Code != http.StatusOK {
				t.Fatalf("token exchange failed: %s", w.Body.String())
			}
			if scenario != "success" && w.Code < 400 {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestInvalidTokenFormsDoNotConsumeCode(t *testing.T) {
	for _, scenario := range []string{"duplicate-code", "duplicate-verifier", "query-code", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			s := New(Config{JWTSecret: []byte(strings.Repeat("s", 32))})
			code, err := s.CreateAuthCode("client", "https://client.example/cb", "user", "session", "challenge")
			if err != nil {
				t.Fatal(err)
			}
			form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {"verifier"}}
			path := "/token"
			switch scenario {
			case "duplicate-code":
				form.Add("code", "another-code")
			case "duplicate-verifier":
				form.Add("code_verifier", "another-verifier")
			case "query-code":
				path += "?code=" + url.QueryEscape(code)
			case "oversized":
				form.Set("padding", strings.Repeat("x", 32<<10))
			}
			r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			NewHandlers(s, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).HandleToken(w, r)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request") {
				t.Fatalf("invalid form accepted: %d %s", w.Code, w.Body.String())
			}
			if _, exists := s.authCodes[code]; !exists {
				t.Fatal("invalid form consumed authorization code")
			}
		})
	}
}

func TestAuthorizeBoundsRetainedQueryValues(t *testing.T) {
	for _, field := range []struct {
		name string
		size int
	}{{"state", 8 << 10}, {"scope", 1024}, {"code_challenge", 128}} {
		t.Run(field.name, func(t *testing.T) {
			q := url.Values{"client_id": {"client"}, "redirect_uri": {"https://client.example/cb"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
			q.Set(field.name, strings.Repeat("x", field.size+1))
			if _, err := ParseAuthorizeRequest(httptest.NewRequest(http.MethodGet, "/authorize?"+q.Encode(), nil)); err == nil {
				t.Fatal("oversized retained value accepted")
			}
			q.Set(field.name, strings.Repeat("x", field.size))
			if _, err := ParseAuthorizeRequest(httptest.NewRequest(http.MethodGet, "/authorize?"+q.Encode(), nil)); err != nil {
				t.Fatal("value at limit rejected", err)
			}
		})
	}
}

func TestAuthorizeCapacityReturnsBusyWithoutEvictingConsent(t *testing.T) {
	h := consentTestHandlers(t, "")
	form, _, client := consentPage(t, h)
	for {
		_, err := h.kcManager.SessionManager().GeneratePendingAuthorization(&pendingAuth{})
		if errors.Is(err, kc.ErrPendingAuthorizationLimit) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	q := url.Values{"client_id": {client}, "redirect_uri": {"https://client.example/callback"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
	w := httptest.NewRecorder()
	h.HandleAuthorize(w, httptest.NewRequest(http.MethodGet, "/authorize?"+q.Encode(), nil))
	assertBrowserError(t, w, http.StatusServiceUnavailable, "Sign-in temporarily unavailable")
	if w.Header().Get("Retry-After") != "60" || len(w.Result().Cookies()) != 0 {
		t.Fatal("busy response starts another flow or has no retry guidance")
	}
	if _, err := h.kcManager.SessionManager().Get(form.Get("flow")); err != nil {
		t.Fatal("busy response evicted existing consent", err)
	}
}
