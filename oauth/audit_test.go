package oauth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/audit"
	"github.com/zerodha/kite-mcp-server/internal/audit/audittest"
)

func TestAuditConsentAndEstablishedLogin(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","data":{"user_id":"AB1234","access_token":"private-access-token","refresh_token":"private-refresh-token"}}`)
	}))
	defer upstream.Close()
	h := consentTestHandlers(t, upstream.URL)
	var logs audittest.Recorder
	h.logger = logs.Logger()
	h.kcManager.Logger = h.logger
	form, c, client := consentPage(t, h)
	approved := postConsent(h, form, c)
	require.Equal(t, 303, approved.Code)
	require.Len(t, logs.Events("client_consent_granted"), 1)
	require.Empty(t, logs.Events("user_login_success"))
	login, err := url.Parse(approved.Header().Get("Location"))
	require.NoError(t, err)
	callbackURL := "/callback?request_token=private-request-token&" + login.Query().Get("redirect_params")
	request := httptest.NewRequest("GET", callbackURL, nil)
	for _, cookie := range approved.Result().Cookies() {
		if strings.Contains(cookie.Name, "-state-") {
			request.AddCookie(cookie)
		}
	}
	response := httptest.NewRecorder()
	h.HandleCallback(response, request)
	require.Equal(t, 302, response.Code)
	records := logs.Events("user_login_success")
	require.Len(t, records, 1)
	require.Equal(t, "AB1234", records[0]["user_id"])
	require.Equal(t, audit.Ref(client), records[0]["client_ref"])
	require.Equal(t, audit.Ref(form.Get("flow")), records[0]["consent_ref"])
	require.Len(t, logs.Events("kite_login_exchange_succeeded"), 1)
	replay := httptest.NewRecorder()
	h.HandleCallback(replay, request)
	require.Len(t, logs.Events("user_login_success"), 1)
	location, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "redirect_uri": {"https://client.example/callback"}, "code": {location.Query().Get("code")}, "code_verifier": {strings.Repeat("v", 43)}}
	tokenReq := httptest.NewRequest("POST", "/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResponse := httptest.NewRecorder()
	h.HandleToken(tokenResponse, tokenReq)
	require.Equal(t, 200, tokenResponse.Code)
	require.Len(t, logs.Events("oauth_token_issued"), 1)
	require.Equal(t, records[0]["session_ref"], logs.Events("oauth_token_issued")[0]["session_ref"])
	encoded, err := json.Marshal(logs.Records())
	require.NoError(t, err)
	for _, secret := range []string{"private-access-token", "private-refresh-token", "private-request-token", form.Get("csrf"), client, location.Query().Get("code")} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestAuditDeniedConsentCannotBecomeSuccessfulLogin(t *testing.T) {
	h := consentTestHandlers(t, "")
	var logs audittest.Recorder
	h.logger = logs.Logger()
	h.kcManager.Logger = h.logger
	form, c, _ := consentPage(t, h)
	form.Set("decision", "deny")
	postConsent(h, form, c)
	postConsent(h, form, c)
	require.Len(t, logs.Events("client_consent_declined"), 1)
	require.Empty(t, logs.Events("client_consent_granted"))
	require.Empty(t, logs.Events("user_login_success"))
	require.NotEmpty(t, logs.Events("oauth_flow_failed"))
}
