package oauth

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/storage"
)

func TestConsentCallbackAndTokenExchangeSurviveRestarts(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"user_id":"test-user","access_token":"access","refresh_token":"refresh"}}`))
	}))
	defer upstream.Close()
	path := filepath.Join(t.TempDir(), "state.db")
	open := func() (*Handlers, *storage.SQLite) {
		s, err := storage.OpenSQLite(path, bytes.Repeat([]byte{6}, 32))
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		return consentTestHandlersWithState(t, upstream.URL, s), s
	}
	h, state := open()
	form, cookie, clientID := consentPage(t, h)
	h.kcManager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	h, state = open()
	approved := postConsent(h, form, cookie)
	require.Equal(t, http.StatusSeeOther, approved.Code, approved.Body.String())
	login, err := url.Parse(approved.Header().Get("Location"))
	require.NoError(t, err)
	callbackURL := "/callback?request_token=test&" + login.Query().Get("redirect_params")
	var browserState *http.Cookie
	for _, c := range approved.Result().Cookies() {
		if strings.Contains(c.Name, "-state-") {
			browserState = c
		}
	}
	require.NotNil(t, browserState)
	h.kcManager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	h, state = open()
	callback := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	callback.AddCookie(browserState)
	result := httptest.NewRecorder()
	h.HandleCallback(result, callback)
	require.Equal(t, http.StatusFound, result.Code, result.Body.String())
	require.EqualValues(t, 1, calls.Load())
	redirect, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)
	exchange := &TokenRequest{Code: redirect.Query().Get("code"), ClientID: clientID, RedirectURI: "https://client.example/callback", CodeVerifier: strings.Repeat("v", 43)}
	h.kcManager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	h, state = open()
	token, err := h.server.ExchangeCode(exchange)
	require.NoError(t, err)
	claims, err := h.server.ValidateToken(token)
	require.NoError(t, err)
	_, err = h.kcManager.SessionExpiry(claims.SessionID)
	require.NoError(t, err)
	h.kcManager.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	h, _ = open()
	_, err = h.server.ExchangeCode(exchange)
	require.Error(t, err, "authorization code must remain consumed")
	replay := httptest.NewRecorder()
	h.HandleCallback(replay, callback)
	require.Equal(t, http.StatusBadRequest, replay.Code)
	require.EqualValues(t, 1, calls.Load(), "callback replay after restart must not exchange Kite credentials")
	claims, err = h.server.ValidateToken(token)
	require.NoError(t, err)
	_, err = h.kcManager.SessionExpiry(claims.SessionID)
	require.NoError(t, err)
}
