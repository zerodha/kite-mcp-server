package oauth

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/zerodha/kite-mcp-server/internal/storage"
)

func (s *Server) RestoreState() error {
	if s.config.State == nil {
		return nil
	}
	values, err := s.config.State.Load("auth_codes")
	if err != nil {
		return err
	}
	for id, data := range values {
		var value AuthCode
		if json.Unmarshal(data, &value) != nil || value.Code != id || value.ExpiresAt.IsZero() {
			return errors.New("invalid stored authorization code")
		}
		if !time.Now().Before(value.ExpiresAt) {
			if err := storage.Delete(s.config.State, "auth_codes", id); err != nil {
				return err
			}
			continue
		}
		s.authCodes.Set(id, &value)
	}
	return nil
}

type consentState struct {
	ClientID, RedirectURI, State, CodeChallenge, Scope string
	CSRF, BrowserState                                 string
	Approved, Consumed                                 bool
}

func (p *pendingAuth) SessionData() (string, any) {
	return "oauth_consent", consentState{p.ClientID, p.RedirectURI, p.State, p.CodeChallenge, p.Scope, p.csrf, p.browserState, p.approved, p.consumed}
}

func (h *Handlers) RestoreState() error {
	return h.kcManager.SessionManager().RegisterSessionData("oauth_consent", func(_ string, data json.RawMessage) (any, error) {
		var value consentState
		if json.Unmarshal(data, &value) != nil || value.ClientID == "" || value.CSRF == "" {
			return nil, errors.New("invalid stored consent flow")
		}
		return &pendingAuth{ClientID: value.ClientID, RedirectURI: value.RedirectURI, State: value.State, CodeChallenge: value.CodeChallenge, Scope: value.Scope, csrf: value.CSRF, browserState: value.BrowserState, approved: value.Approved, consumed: value.Consumed}, nil
	})
}
