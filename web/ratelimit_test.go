package web

import (
	"fmt"
	"net/http"
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

func TestPeerFloodDoesNotResetExistingRateLimits(t *testing.T) {
	limiter := NewRateLimiter()
	defer limiter.Shutdown()
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func(ip string) int {
		r := httptest.NewRequest(http.MethodPost, "/token", nil)
		r.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 5; i++ {
		if got := request("192.0.2.1"); got != http.StatusNoContent {
			t.Fatalf("initial request rejected: %d", got)
		}
	}
	for i := 1; i < maxRateLimitPeers; i++ {
		limiter.getLimiter(fmt.Sprintf("peer-%d", i))
	}
	for i := 0; i < 5; i++ {
		if got := request("198.51.100.1"); got != http.StatusNoContent {
			t.Fatalf("overflow request rejected before shared burst was used: %d", got)
		}
	}
	if got := request("198.51.100.1"); got != http.StatusTooManyRequests {
		t.Fatalf("overflow peer reset its quota: %d", got)
	}
	// An unrelated new peer must not be forced into the exhausted bucket.
	var independent string
	for i := 2; i < 255; i++ {
		ip := fmt.Sprintf("198.51.%d.1", i)
		if limiter.getLimiter(ip) != limiter.getLimiter("198.51.100.1") {
			independent = ip
			break
		}
	}
	if independent == "" || request(independent) != http.StatusNoContent {
		t.Fatal("all unseen peers share one exhausted bucket")
	}
	if got := request("192.0.2.1"); got != http.StatusTooManyRequests {
		t.Fatalf("peer flood reset the existing client's quota: %d", got)
	}
	if got := limiter.limiters.Len(); got != maxRateLimitPeers {
		t.Fatalf("peer flood grew retained entries to %d", got)
	}
}

func TestIPv6PrivacyAddressesShareQuota(t *testing.T) {
	l := NewRateLimiter()
	defer l.Shutdown()
	first := l.getLimiter("2001:db8:1:2::1")
	for i := 0; i < 10000; i++ {
		if l.getLimiter(fmt.Sprintf("2001:db8:1:2::%x", i)) != first {
			t.Fatal("rotating /64 privacy address reset quota")
		}
	}
	for range 5 {
		if !first.Allow() {
			t.Fatal("missing initial burst")
		}
	}
	if l.getLimiter("2001:db8:1:2::ffff").Allow() {
		t.Fatal("privacy address bypass")
	}
	if !l.getLimiter("2001:db8:1:3::1").Allow() {
		t.Fatal("unrelated /64 blocked")
	}
	if l.getLimiter("::ffff:192.0.2.1") != l.getLimiter("192.0.2.1") {
		t.Fatal("IPv4 mapped alias bypass")
	}
	if l.limiters.Len() != 3 {
		t.Fatal("privacy addresses grew table", l.limiters.Len())
	}
}
