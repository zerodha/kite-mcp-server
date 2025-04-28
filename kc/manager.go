package kc

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

var (
	ErrSessionNotFound  = errors.New("session not found, try to login again")
	ErrInvalidSessionID = errors.New("invalid session id, please try logging in again")
)

type SessionData struct {
	Kite *KiteConnect
}

type Manager struct {
	apiKey    string
	apiSecret string

	Instruments *instruments.Manager

	Sessions map[string]*SessionData
}

func NewManager(apiKey, apiSecret string) *Manager {
	return &Manager{
		apiKey:    apiKey,
		apiSecret: apiSecret,

		Instruments: instruments.NewManager(),

		Sessions: make(map[string]*SessionData),
	}
}

func (m *Manager) GetSession(sessionID string) (*SessionData, error) {
	if sessionID == "" {
		return nil, ErrSessionNotFound
	}

	kc, ok := m.Sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	return kc, nil
}

func (m *Manager) ClearSession(sessionID string) {
	if sessionID == "" {
		return
	}

	if sess, ok := m.Sessions[sessionID]; ok {
		sess.Kite.Client.InvalidateAccessToken()
		delete(m.Sessions, sessionID)
	}
}

func (m *Manager) SessionLoginURL(sessionID string) (string, error) {
	if sessionID == "" {
		return "", ErrInvalidSessionID
	}

	kc := NewKiteConnect(m.apiKey)
	m.Sessions[sessionID] = &SessionData{
		Kite: kc,
	}

	redirectParams := url.QueryEscape("session_id=" + sessionID) // TODO: maybe we can hash/salt this for added security

	return kc.Client.GetLoginURL() + "&redirect_params=" + redirectParams, nil
}

func (m *Manager) GenerateSession(sessionID, requestToken string) error {
	if sessionID == "" {
		return ErrInvalidSessionID
	}

	// check if session exists else return an error
	sess, ok := m.Sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}

	userSess, err := sess.Kite.Client.GenerateSession(requestToken, m.apiSecret)
	if err != nil {
		return fmt.Errorf("failed to generate session: %w", err)
	}

	sess.Kite.Client.SetAccessToken(userSess.AccessToken)

	return nil
}

func (m *Manager) HandleKiteCallback() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		requestToken := r.URL.Query()["request_token"][0]
		sessionID := r.URL.Query()["session_id"][0] // TODO: think of hashing this with some secret so that it cant be tampered.

		if sessionID == "" || requestToken == "" {
			log.Println("missing session_id or request_token")
			http.Error(w, "missing session_id or request_token", http.StatusBadRequest)
			return
		}

		if err := m.GenerateSession(sessionID, requestToken); err != nil {
			log.Println("error generating session", err)
			http.Error(w, "error generating session", http.StatusInternalServerError)
			return
		}

		w.Write([]byte("login successful!"))
		return
	}
}
