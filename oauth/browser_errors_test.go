package oauth

import (
	"bytes"
	"fmt"
	"github.com/zerodha/kite-mcp-server/web"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestBrowserAuthorizationErrors(t *testing.T) {
	h := consentTestHandlers(t, "")
	for _, tc := range []struct {
		name, method, path, body, want string
		status                         int
	}{
		{"invalid authorize", "GET", "/authorize", "", "Invalid login request", 400},
		{"unsupported method", "PUT", "/authorize", "", "Invalid login request", 405},
		{"missing consent", "POST", "/authorize", "flow=missing", "Login expired", 400},
		{"invalid callback", "GET", "/callback?session_id=invalid", "", "Unable to sign in", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			if strings.HasPrefix(tc.path, "/authorize") {
				h.HandleAuthorize(w, r)
			} else {
				h.HandleCallback(w, r)
			}
			assertBrowserError(t, w, tc.status, tc.want)
		})
	}
}

func assertBrowserError(t *testing.T, w *httptest.ResponseRecorder, status int, want string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d", w.Code, status)
	}
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("incorrect headers: %v", w.Header())
	}
	for _, text := range []string{"<!doctype html>", want, "return to your MCP client", "data:image/svg+xml"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatalf("missing %q", text)
		}
	}
}

func TestKiteLoginFailurePage(t *testing.T) {
	for _, tc := range []struct {
		status, wantStatus int
		title              string
		plain              bool
		sdkStatus          int
		sdkType            string
	}{
		{429, 429, "Kite login temporarily limited", false, 429, "GeneralException"},
		{500, 502, "Unable to connect to Kite", false, 500, "GeneralException"},
		{429, 429, "Kite login temporarily limited", true, 504, "DataException"},
		{0, 502, "Unable to connect to Kite", false, 503, "NetworkException"},
	} {
		t.Run(fmt.Sprintf("status=%d/plain=%v", tc.status, tc.plain), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.plain {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(tc.status)
					if _, err := fmt.Fprint(w, "Too many requests"); err != nil {
						t.Errorf("write upstream response: %v", err)
					}
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, `{"status":"error","error_type":"GeneralException","message":"private-upstream-detail"}`); err != nil {
					t.Errorf("write upstream response: %v", err)
				}
			}))
			defer upstream.Close()
			if tc.status == 0 {
				upstream.Close()
			}
			h := consentTestHandlers(t, upstream.URL)
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			h.logger = logger
			h.kcManager.Logger = logger
			form, c, _ := consentPage(t, h)
			consent := postConsent(h, form, c)
			login, _ := url.Parse(consent.Header().Get("Location"))
			r := httptest.NewRequest("GET", "/callback?request_token=sensitive-token&"+login.Query().Get("redirect_params"), nil)
			for _, cookie := range consent.Result().Cookies() {
				if strings.Contains(cookie.Name, "-state-") {
					r.AddCookie(cookie)
				}
			}
			w := httptest.NewRecorder()
			h.HandleCallback(w, r)
			assertBrowserError(t, w, tc.wantStatus, tc.title)
			for _, field := range []string{`"stage":"kite_token_exchange"`, `"sdk_status":` + fmt.Sprint(tc.sdkStatus), `"sdk_error_type":"` + tc.sdkType + `"`, `"duration_ms":`, `"session_id":"` + form.Get("flow") + `"`, `"path":"/callback"`} {
				if !strings.Contains(logs.String(), field) {
					t.Fatalf("missing log field %s: %s", field, logs.String())
				}
			}
			if tc.status == 0 {
				if strings.Contains(logs.String(), `"upstream_status"`) || !strings.Contains(logs.String(), `"failure_kind":"transport_error"`) {
					t.Fatalf("transport failure reported HTTP response: %s", logs.String())
				}
			} else if !strings.Contains(logs.String(), `"upstream_status":`+fmt.Sprint(tc.status)) {
				t.Fatalf("missing actual status: %s", logs.String())
			}

			for _, secret := range []string{"sensitive-token", "test-secret", "test-key", "request_token=", "redirect_params="} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("logs leaked %s", secret)
				}
			}
			for _, secret := range []string{"private-upstream-detail", "sensitive-token", "test-secret"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatalf("leaked %s", secret)
				}
			}
			if _, err := h.kcManager.SessionManager().Get(form.Get("flow")); err == nil {
				t.Fatal("failed flow was not cleaned up")
			}
		})
	}
}

func TestAuthorizeMiddlewareRateLimitPage(t *testing.T) {
	h := consentTestHandlers(t, "")
	limiter := web.NewRateLimiter()
	synctest.Test(t, func(t *testing.T) {
		reached := 0
		handler := limiter.MiddlewareWithRejection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++; w.WriteHeader(204) }), h.HandleAuthorizeRateLimit)
		for i := 0; i < 5; i++ {
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/authorize", nil))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/authorize", nil))
		assertBrowserError(t, w, 429, "Too many login attempts")
		if reached != 5 || w.Header().Get("Retry-After") != "12" {
			t.Fatalf("incorrect middleware behavior: reached=%d headers=%v", reached, w.Header())
		}
		time.Sleep(2 * time.Hour)
	})
}

// Simulate a response that accepts some bytes and then loses its connection.
type partialResponseWriter struct {
	header   http.Header
	statuses []int
	writes   int
}

func (w *partialResponseWriter) Header() http.Header    { return w.header }
func (w *partialResponseWriter) WriteHeader(status int) { w.statuses = append(w.statuses, status) }
func (w *partialResponseWriter) Write(p []byte) (int, error) {
	w.writes++
	return min(16, len(p)), io.ErrUnexpectedEOF
}

func TestBrowserWriteFailureDoesNotAppendFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, `{"status":"success","data":{"user_id":"user","access_token":"access"}}`); err != nil {
			t.Errorf("write upstream response: %v", err)
		}
	}))
	defer upstream.Close()
	for _, kind := range []string{"error", "success", "consent"} {
		t.Run(kind, func(t *testing.T) {
			h := consentTestHandlers(t, upstream.URL)
			var logs bytes.Buffer
			h.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			w := &partialResponseWriter{header: make(http.Header)}
			wantStatus := 400
			switch kind {
			case "error":
				h.HandleCallback(w, httptest.NewRequest("GET", "/callback?session_id=invalid", nil))
			case "success":
				wantStatus = 200
				sessionID := h.kcManager.SessionManager().Generate()
				signed, err := h.kcManager.SessionSigner().SignRedirectParams(sessionID)
				if err != nil {
					t.Fatal(err)
				}
				h.HandleCallback(w, httptest.NewRequest("GET", "/callback?request_token=token&"+signed, nil))
			case "consent":
				wantStatus = 200
				id, err := h.server.RegisterClient([]string{"https://client.example/callback"})
				if err != nil {
					t.Fatal(err)
				}
				q := url.Values{"client_id": {id}, "redirect_uri": {"https://client.example/callback"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
				h.HandleAuthorize(w, httptest.NewRequest("GET", "/authorize?"+q.Encode(), nil))
			}
			if w.writes != 1 || len(w.statuses) != 1 || w.statuses[0] != wantStatus {
				t.Fatalf("fallback attempted after write failure: writes=%d statuses=%v", w.writes, w.statuses)
			}
			if !strings.Contains(logs.String(), "unexpected EOF") {
				t.Fatalf("write failure not logged: %s", logs.String())
			}
		})
	}
}
