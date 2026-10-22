package oauth

import (
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestSignedClientName(t *testing.T) {
	s := New(Config{JWTSecret: []byte(strings.Repeat("s", 32))})
	for _, name := range []string{"", "Example Client", strings.Repeat("<", maxDCRClientName)} {
		redirects := make([]string, maxDCRRedirectURIs)
		for i := range redirects {
			redirects[i] = fmt.Sprintf("https://example.com/%d", i)
		}
		id, err := s.RegisterClient(redirects, name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.ClientName(id)
		if err != nil || got != name {
			t.Fatalf("ClientName = %q, %v; want %q", got, err, name)
		}
		if err := s.ValidateClientRedirect(id, redirects[0]); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ClientName(id + "x"); err == nil {
			t.Fatal("tampered client name accepted")
		}
	}
	legacy, err := s.RegisterClient([]string{"https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if name, err := s.ClientName(legacy); err != nil || name != "" {
		t.Fatalf("legacy ClientName = %q, %v", name, err)
	}
	if _, err := s.RegisterClient([]string{"https://example.com"}, strings.Repeat("x", maxDCRClientName+1)); err == nil {
		t.Fatal("oversized client name accepted")
	}
}

func TestAbandonedAuthCodesExpireOnActivity(t *testing.T) {
	for _, activity := range []string{"create", "exchange"} {
		t.Run(activity, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := New(Config{AuthCodeTTL: time.Minute})
				abandoned, err := s.CreateAuthCode("client", "https://example.com", "user", "session", "challenge")
				if err != nil {
					t.Fatal(err)
				}
				time.Sleep(30 * time.Second)
				live, err := s.CreateAuthCode("client", "https://example.com", "user", "session", "challenge")
				if err != nil {
					t.Fatal(err)
				}
				time.Sleep(30 * time.Second)
				if activity == "create" {
					if _, err := s.CreateAuthCode("client", "https://example.com", "user", "session", "challenge"); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := s.ExchangeCode(&TokenRequest{Code: "unknown"}); err == nil {
						t.Fatal("unknown code accepted")
					}
				}
				if _, exists := s.authCodes[abandoned]; exists {
					t.Fatal("expired abandoned code retained")
				}
				if _, exists := s.authCodes[live]; !exists {
					t.Fatal("live code removed by cleanup")
				}
			})
		})
	}
}

func TestAuthCodeCapacityPreservesOutstandingCodes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(Config{AuthCodeTTL: time.Minute})
		for i := range maxPendingAuthCodes {
			key := fmt.Sprint(i)
			s.authCodes[key] = &AuthCode{Code: key, ExpiresAt: time.Now().Add(time.Minute)}
		}
		if code, err := s.CreateAuthCode("client", "https://example.com", "user", "session", "challenge"); err == nil || code != "" {
			t.Fatal("full store accepted another code")
		}
		if len(s.authCodes) != maxPendingAuthCodes {
			t.Fatal("capacity rejection evicted outstanding codes")
		}
		time.Sleep(time.Minute)
		if _, err := s.CreateAuthCode("client", "https://example.com", "user", "session", "challenge"); err != nil {
			t.Fatal(err)
		}
		if len(s.authCodes) != 1 {
			t.Fatalf("expired codes not reclaimed: %d", len(s.authCodes))
		}
	})
}

func TestAuthCodeCleanupIsThrottled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(Config{})
		s.lastAuthCodeCleanup = time.Now()
		s.authCodes["expired"] = &AuthCode{ExpiresAt: time.Now().Add(-time.Second)}
		_, _ = s.ExchangeCode(&TokenRequest{Code: "unknown"})
		if _, exists := s.authCodes["expired"]; !exists {
			t.Fatal("cleanup was not throttled")
		}
		time.Sleep(authCodeCleanupInterval)
		_, _ = s.ExchangeCode(&TokenRequest{Code: "unknown"})
		if len(s.authCodes) != 0 {
			t.Fatal("cleanup did not resume after interval")
		}
	})
}
