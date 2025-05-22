package kc

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sync"

	"github.com/zerodha/kite-mcp-server/kc/instruments"
	"github.com/zerodha/kite-mcp-server/kc/templates"
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

	templates map[string]*template.Template

	Instruments *instruments.Manager
	sessions    map[string]*SessionData
	sessLock    sync.RWMutex
}

func NewManager(apiKey, apiSecret string) *Manager {
	templates, err := setupTemplates()
	if err != nil {
		log.Fatal(err)
	}

	return &Manager{
		apiKey:    apiKey,
		apiSecret: apiSecret,

		templates: templates,

		Instruments: instruments.NewManager(),
		sessions:    make(map[string]*SessionData),
	}
}

func (m *Manager) GetSession(sessionID string) (*SessionData, error) {
	if sessionID == "" {
		return nil, ErrSessionNotFound
	}

	m.sessLock.RLock()
	kc, ok := m.sessions[sessionID]
	m.sessLock.RUnlock()
	if !ok {
		return nil, ErrSessionNotFound
	}

	return kc, nil
}

func (m *Manager) ClearSession(sessionID string) {
	if sessionID == "" {
		return
	}

	m.sessLock.Lock()
	if sess, ok := m.sessions[sessionID]; ok {
		sess.Kite.Client.InvalidateAccessToken()
		delete(m.sessions, sessionID)
	}
	m.sessLock.Unlock()
}

func (m *Manager) SessionLoginURL(sessionID string) (string, error) {
	if sessionID == "" {
		return "", ErrInvalidSessionID
	}

	kc := NewKiteConnect(m.apiKey)
	m.sessLock.Lock()
	m.sessions[sessionID] = &SessionData{
		Kite: kc,
	}
	m.sessLock.Unlock()

	redirectParams := url.QueryEscape("session_id=" + sessionID) // TODO: maybe we can hash/salt this for added security

	return kc.Client.GetLoginURL() + "&redirect_params=" + redirectParams, nil
}

func (m *Manager) GenerateSession(sessionID, requestToken string) error {
	if sessionID == "" {
		return ErrInvalidSessionID
	}

	// check if session exists else return an error
	m.sessLock.RLock()
	sess, ok := m.sessions[sessionID]
	m.sessLock.RUnlock()
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

func setupTemplates() (map[string]*template.Template, error) {
	out := map[string]*template.Template{}

	templateList := []string{"index.html"}

	for _, templateName := range templateList {
		templ, err := template.ParseFS(templates.FS, templateName)
		if err != nil {
			return out, fmt.Errorf("error parsing %s: %w", templateName, err)
		}
		out[templateName] = templ
	}

	return out, nil
}

func (m *Manager) HandleKiteCallback() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		qVals := r.URL.Query()
		requestToken := qVals.Get("request_token")
		sessionID := qVals.Get("session_id")
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

		templ, ok := m.templates["index.html"]
		if !ok {
			log.Println("template not found")
			http.Error(w, "template not found", http.StatusInternalServerError)
			return
		}

		err := templ.Execute(w, nil)
		if err != nil {
			log.Println("error executing template", err)
			http.Error(w, "error executing template", http.StatusInternalServerError)
			return
		}

		return
	}
}
