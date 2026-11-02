package oauth

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
