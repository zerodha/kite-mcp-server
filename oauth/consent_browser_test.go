package oauth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Opt-in browser regression: KITE_TEST_CHROMIUM=/path/to/chromium go test
// ./oauth -run TestConsentBrowserRedirects -v. All navigation stays on local
// mock servers. Unlike recorder tests, Chromium enforces the form's CSP.
func TestConsentBrowserRedirects(t *testing.T) {
	browser := os.Getenv("KITE_TEST_CHROMIUM")
	if browser == "" {
		t.Skip("set KITE_TEST_CHROMIUM to run real browser consent tests")
	}
	for _, decision := range []string{"approve", "deny"} {
		t.Run(decision, func(t *testing.T) {
			marker := "consent-browser-" + decision + "-complete"
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if decision == "deny" && (r.URL.Query().Get("error") != "access_denied" || r.URL.Query().Get("state") != "browser-client-state") {
					t.Error("denial did not preserve OAuth error and client state")
					http.Error(w, "invalid denial", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				io.WriteString(w, "<!doctype html><html><body>"+marker+"</body></html>")
			}))
			defer destination.Close()
			h := consentTestHandlers(t, "")
			auth := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/authorize" {
					http.NotFound(w, r)
					return
				}
				response := httptest.NewRecorder()
				h.HandleAuthorize(response, r)
				for name, values := range response.Header() {
					for _, value := range values {
						w.Header().Add(name, value)
					}
				}
				if r.Method == http.MethodPost && decision == "approve" && response.Code == http.StatusSeeOther {
					// Stand in for the external Kite login page without contacting Kite.
					// Keep the cross-origin redirect, cookies, and originating CSP intact.
					login, err := url.Parse(response.Header().Get("Location"))
					if err != nil || login.Host != "kite.zerodha.com" || login.Query().Get("redirect_params") == "" {
						t.Error("approval did not produce a Kite login redirect")
						http.Error(w, "invalid login redirect", http.StatusInternalServerError)
						return
					}
					w.Header().Set("Location", destination.URL+"/kite")
				}
				w.WriteHeader(response.Code)
				w.Write(response.Body.Bytes())
				if r.Method == http.MethodGet && response.Code == http.StatusOK {
					// Act like a user pressing the real form button. The production template
					// and headers remain unchanged; no CSP protections are bypassed.
					io.WriteString(w, `<script>addEventListener('load',()=>document.querySelector('button[value="`+decision+`"]')?.click())</script>`)
				}
			}))
			h.server.config.Issuer = "http://" + auth.Listener.Addr().String()
			// Registration signatures bind the issuer, so register after setting it.
			clientID, err := h.server.RegisterClient([]string{destination.URL + "/callback"}, "Browser test client")
			if err != nil {
				t.Fatal(err)
			}
			auth.Start()
			defer auth.Close()
			q := url.Values{"client_id": {clientID}, "redirect_uri": {destination.URL + "/callback"}, "response_type": {"code"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}, "state": {"browser-client-state"}}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, browser, "--headless", "--no-sandbox", "--disable-background-networking", "--disable-extensions", "--disable-sync", "--disable-dev-shm-usage", "--no-first-run", "--no-proxy-server", "--user-data-dir="+t.TempDir(), "--virtual-time-budget=2000", "--dump-dom", auth.URL+"/authorize?"+q.Encode())
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Chromium failed: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), marker) {
				t.Fatalf("browser did not complete %s redirect (check CSP); response tail: %s", decision, string(output[max(0, len(output)-800):]))
			}
		})
	}
}
