package oauth

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestOrderBrowserProofIsSeparateFromBearer(t *testing.T) {
	active := true
	s := New(Config{Issuer: "https://mcp.example", JWTSecret: []byte(strings.Repeat("s", 32)), SessionExpiry: func(string) (time.Time, error) {
		if !active {
			return time.Time{}, errors.New("logged out")
		}
		return time.Now().Add(time.Hour), nil
	}})
	w := httptest.NewRecorder()
	require.NoError(t, s.SetOrderBrowserSession(w, "grant", "USER"))
	cookie := w.Result().Cookies()[0]
	require.True(t, cookie.Secure)
	require.True(t, cookie.HttpOnly)
	require.True(t, strings.HasPrefix(cookie.Name, "__Host-"))
	r := httptest.NewRequest("GET", "https://mcp.example/order-approvals/id", nil)
	r.AddCookie(cookie)
	require.NoError(t, s.VerifyOrderBrowserSession(r, "grant", "USER"))
	require.Error(t, s.VerifyOrderBrowserSession(r, "grant", "OTHER"))
	require.Error(t, s.VerifyOrderBrowserSession(r, "other-grant", "USER"))
	_, err := s.ValidateToken(cookie.Value)
	require.Error(t, err, "browser cookie is not a bearer token")
	bearer, err := s.generateAccessToken("USER", "grant", "registered-client")
	require.NoError(t, err)
	claims, err := s.ValidateToken(bearer)
	require.NoError(t, err)
	require.Equal(t, "registered-client", claims.ClientID)
	cookie.Value = bearer
	r = httptest.NewRequest("GET", "https://mcp.example/order-approvals/id", nil)
	r.AddCookie(cookie)
	require.Error(t, s.VerifyOrderBrowserSession(r, "grant", "USER"), "bearer must not grant browser approval authority")
	active = false
	require.Error(t, s.VerifyOrderBrowserSession(r, "grant", "USER"))
}

func TestOrderBrowserRejectsForgedAndMisboundProofs(t *testing.T) {
	for _, scenario := range []string{"signature", "algorithm", "issuer", "audience", "purpose", "session", "account", "expired", "missing-expiry"} {
		t.Run(scenario, func(t *testing.T) {
			secret := []byte(strings.Repeat("s", 32))
			s := New(Config{Issuer: "https://mcp.example", JWTSecret: secret, SessionExpiry: func(string) (time.Time, error) {
				return time.Now().Add(time.Hour), nil
			}})
			claims := orderBrowserClaims{RegisteredClaims: jwt.RegisteredClaims{
				Issuer: "https://mcp.example", Subject: "USER", Audience: jwt.ClaimStrings{"https://mcp.example/order-approvals"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			}, SessionID: "grant", Purpose: "order_browser"}
			var method jwt.SigningMethod = jwt.SigningMethodHS256
			switch scenario {
			case "signature":
				secret = []byte(strings.Repeat("a", 32))
			case "algorithm":
				method = jwt.SigningMethodHS512
			case "issuer":
				claims.Issuer = "https://attacker.example"
			case "audience":
				claims.Audience = jwt.ClaimStrings{"https://mcp.example/mcp"}
			case "purpose":
				claims.Purpose = "access_token"
			case "session":
				claims.SessionID = "other-grant"
			case "account":
				claims.Subject = "OTHER"
			case "expired":
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			case "missing-expiry":
				claims.ExpiresAt = nil
			}
			value, err := jwt.NewWithClaims(method, claims).SignedString(secret)
			require.NoError(t, err)
			name, _ := s.orderBrowserCookie("grant")
			r := httptest.NewRequest("GET", "https://mcp.example/order-approvals/id", nil)
			r.Header.Set("Cookie", name+"="+value)
			require.Error(t, s.VerifyOrderBrowserSession(r, "grant", "USER"))
		})
	}
}
