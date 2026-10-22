package oauth

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenAudienceAndIssuerBinding(t *testing.T) {
	s := New(Config{Issuer: "https://mcp.example.com", JWTSecret: []byte(strings.Repeat("s", 32))})
	issued, err := s.generateAccessToken("user", "session")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := s.ValidateToken(issued)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "https://mcp.example.com/mcp" {
		t.Fatalf("incorrect audience: %v", claims.Audience)
	}
	for _, tc := range []struct {
		name, issuer, audience string
		expiry                 bool
	}{
		{"missing audience", s.config.Issuer, "", true},
		{"foreign audience", s.config.Issuer, "https://other.example.com/mcp", true},
		{"foreign issuer", "https://other.example.com", s.resourceURI(), true},
		{"missing expiry", s.config.Issuer, s.resourceURI(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := TokenClaims{RegisteredClaims: jwt.RegisteredClaims{Issuer: tc.issuer}}
			if tc.audience != "" {
				claims.Audience = jwt.ClaimStrings{tc.audience}
			}
			if tc.expiry {
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
			}
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.config.JWTSecret)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.ValidateToken(token); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
}

func TestResourceIndicators(t *testing.T) {
	s := New(Config{Issuer: "https://mcp.example.com"})
	for _, resource := range []string{"", s.resourceURI(), "HTTPS://MCP.EXAMPLE.COM/mcp"} {
		if err := s.ValidateResource(resource); err != nil {
			t.Fatalf("valid resource %q rejected: %v", resource, err)
		}
	}
	for _, resource := range []string{"https://other.example.com/mcp", "https://mcp.example.com", s.resourceURI() + "#", s.resourceURI() + "?x=1", "mcp.example.com/mcp"} {
		if err := s.ValidateResource(resource); err == nil {
			t.Fatalf("foreign resource %q accepted", resource)
		}
	}
	q := url.Values{"client_id": {"client"}, "redirect_uri": {"https://client.example.com/cb"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}, "resource": {s.resourceURI()}}
	r, _ := http.NewRequest(http.MethodGet, "https://mcp.example.com/authorize?"+q.Encode(), nil)
	auth, err := ParseAuthorizeRequest(r)
	if err != nil || auth.Resource != s.resourceURI() {
		t.Fatalf("authorization resource not parsed: %v", err)
	}
	q.Add("resource", "https://other.example.com")
	r, _ = http.NewRequest(http.MethodGet, "https://mcp.example.com/authorize?"+q.Encode(), nil)
	if _, err := ParseAuthorizeRequest(r); err == nil {
		t.Fatal("multiple authorization resources accepted")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {"code"}, "code_verifier": {"verifier"}, "resource": {s.resourceURI()}}
	r, _ = http.NewRequest(http.MethodPost, "https://mcp.example.com/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	token, err := ParseTokenRequest(r)
	if err != nil || token.Resource != s.resourceURI() {
		t.Fatalf("token resource not parsed: %v", err)
	}
	form.Add("resource", "https://other.example.com")
	r, _ = http.NewRequest(http.MethodPost, "https://mcp.example.com/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := ParseTokenRequest(r); err == nil {
		t.Fatal("multiple token resources accepted")
	}
}

func TestForeignResourceDoesNotConsumeCode(t *testing.T) {
	s := New(Config{Issuer: "https://mcp.example.com"})
	code, err := s.CreateAuthCode("client", "https://client.example.com", "user", "session", "challenge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExchangeCode(&TokenRequest{Code: code, Resource: "https://other.example.com"}); err == nil {
		t.Fatal("foreign resource accepted")
	}
	if _, exists := s.authCodes[code]; !exists {
		t.Fatal("invalid resource consumed outstanding grant")
	}
}
