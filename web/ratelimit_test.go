package web

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPUsesForwardedAddressOnlyFromTrustedProxy(t *testing.T) {
	t.Run("untrusted peer cannot choose key", func(t *testing.T) {
		limiter := NewRateLimiter("10.0.0.0/8")
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.10")
		if got := limiter.clientIP(req); got != "192.0.2.10" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("trusted peer uses forwarded client", func(t *testing.T) {
		limiter := NewRateLimiter("10.0.0.0/8")
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.1.2.3:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.5, 198.51.100.10, 10.2.3.4")
		if got := limiter.clientIP(req); got != "198.51.100.10" {
			t.Fatalf("got %q", got)
		}
	})
}
