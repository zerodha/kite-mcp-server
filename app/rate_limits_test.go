package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerodha/kite-mcp-server/app/metrics"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
	"github.com/zerodha/kite-mcp-server/oauth"
	"github.com/zerodha/kite-mcp-server/web"
)

func TestOAuthRateLimitOverride(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		name := "enabled"
		if disabled {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			logger := testLogger()
			manager, err := kc.New(kc.Config{APIKey: "test-key", APISecret: "test-secret", Logger: logger, Instruments: &instruments.Manager{}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(manager.SessionManager().StopCleanupRoutine)
			server := oauth.New(oauth.Config{Issuer: "https://mcp.example", JWTSecret: []byte(strings.Repeat("s", 32)), DisableRateLimits: disabled})
			limiter := web.NewRateLimiter()
			t.Cleanup(limiter.Shutdown)
			app := &App{Config: &Config{DisableOAuthRateLimits: disabled}, metrics: metrics.New(metrics.Config{}), rateLimiter: limiter, jwtOauthHandlers: oauth.NewHandlers(server, manager, logger)}
			mux := app.setupMux()
			for _, path := range []string{"/authorize", "/token", "/callback", "/register"} {
				// Exhaust every normal IP quota. Invalid requests must still be rejected
				// by protocol validation when the rate-limit override is active.
				last := 0
				for i := 0; i < 110; i++ {
					req := httptest.NewRequest(http.MethodGet, path, nil)
					if path == "/token" || path == "/register" {
						req = httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
					}
					req.RemoteAddr = "198.51.100.88:1234"
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, req)
					last = w.Code
					if disabled && (last == http.StatusTooManyRequests || last < 400 || w.Header().Get("Retry-After") != "") {
						t.Fatalf("%s request %d: status=%d headers=%v", path, i, last, w.Header())
					}
				}
				if !disabled && last != http.StatusTooManyRequests {
					t.Fatalf("%s: expected normal rate limit, got %d", path, last)
				}
			}
		})
	}
}

func TestOAuthRateLimitsRequireExplicitOverride(t *testing.T) {
	for _, value := range []string{"", "false", "true", "typo"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DISABLE_OAUTH_RATE_LIMITS", value)
			if got := NewApp(testLogger()).Config.DisableOAuthRateLimits; got != (value == "true") {
				t.Fatalf("override=%q, disabled=%v", value, got)
			}
		})
	}
}
