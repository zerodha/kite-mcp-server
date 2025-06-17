package kc

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/app/metrics"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
	"github.com/zerodha/kite-mcp-server/kc/templates"
)

// Config holds configuration for creating a new kc Manager
type Config struct {
	APIKey      string
	APISecret   string
	Logger      *slog.Logger
	Metrics     *metrics.Manager
	Instruments *instruments.Manager
}

// New creates a new kc Manager with the given configuration
func New(cfg Config) (*Manager, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("APIKey is required")
	}
	if cfg.APISecret == "" {
		return nil, errors.New("APISecret is required")
	}
	if cfg.Logger == nil {
		return nil, errors.New("logger is required")
	}
	if cfg.Instruments == nil {
		return nil, errors.New("instruments manager is required")
	}

	m := &Manager{
		apiKey:      cfg.APIKey,
		apiSecret:   cfg.APISecret,
		Logger:      cfg.Logger,
		metrics:     cfg.Metrics,
		Instruments: cfg.Instruments,
	}

	if err := m.initializeTemplates(); err != nil {
		return nil, fmt.Errorf("failed to initialize Kite manager: %w", err)
	}

	if err := m.initializeSessionSigner(); err != nil {
		return nil, fmt.Errorf("failed to initialize session signer: %w", err)
	}

	m.initializeSessionManager()
	return m, nil
}

// KiteCredentials holds the short-lived Kite Connect access token and user info.
type KiteCredentials struct {
	AccessToken string
	UserID      string
	ExpiresAt   time.Time
}

const (
	indexTemplate = "login_success.html"
)

// Manager orchestrates Kite Connect interactions and session management.
type Manager struct {
	apiKey         string
	apiSecret      string
	Logger         *slog.Logger
	metrics        *metrics.Manager
	templates      map[string]*template.Template
	sessionManager *SessionManager
	sessionSigner  *SessionSigner
	Instruments    *instruments.Manager
}

func (m *Manager) initializeTemplates() error {
	templates, err := setupTemplates()
	if err != nil {
		return fmt.Errorf("failed to setup templates: %w", err)
	}
	m.templates = templates
	return nil
}

func (m *Manager) initializeSessionSigner() error {
	signer, err := NewSessionSigner()
	if err != nil {
		return fmt.Errorf("failed to create session signer: %w", err)
	}
	m.sessionSigner = signer
	return nil
}

func (m *Manager) initializeSessionManager() {
	sessionManager := NewSessionManager(m.Logger)
	sessionManager.AddCleanupHook(func(s *Session) {
		m.Logger.Info("Cleaning up session", "session_id", s.ID)
	})
	sessionManager.StartCleanupRoutine(context.Background())
	m.sessionManager = sessionManager
}

// GetAuthenticatedClient is the single entry point for tools to get a valid Kite client.
func (m *Manager) GetAuthenticatedClient(sessionID string) (*kiteconnect.Client, error) {
	if sessionID == "" {
		return nil, errors.New("invalid session ID")
	}

	session, _, err := m.sessionManager.GetOrCreate(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create session: %w", err)
	}

	if session.Credentials == nil {
		return nil, errors.New("not logged into Kite. Please use the login tool")
	}

	if time.Now().After(session.Credentials.ExpiresAt) {
		return nil, errors.New("Kite session has expired (24-hour limit). Please use the login tool to refresh")
	}

	client := kiteconnect.New(m.apiKey)
	client.SetAccessToken(session.Credentials.AccessToken)
	return client, nil
}

// CompleteLogin exchanges a request token for a new set of Kite credentials.
func (m *Manager) CompleteLogin(requestToken string) (*KiteCredentials, error) {
	kc := kiteconnect.New(m.apiKey)
	userSess, err := kc.GenerateSession(requestToken, m.apiSecret)
	if err != nil {
		m.Logger.Error("Failed to generate Kite session from request token", "error", err)
		return nil, fmt.Errorf("failed to generate Kite session: %w", err)
	}

	creds := &KiteCredentials{
		AccessToken: userSess.AccessToken,
		UserID:      userSess.UserID,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	if m.metrics != nil {
		m.metrics.TrackDailyUser(creds.UserID)
		m.metrics.Increment("user_logins")
	}

	m.Logger.Info("Successfully completed login and created new Kite credentials", "user_id", creds.UserID)
	return creds, nil
}

// GenerateLoginURL creates a login URL tied to a specific session ID.
func (m *Manager) GenerateLoginURL(sessionID string) (string, error) {
	signedParams, err := m.sessionSigner.SignRedirectParams(sessionID)
	if err != nil {
		m.Logger.Error("Failed to sign redirect params for session", "session_id", sessionID, "error", err)
		return "", fmt.Errorf("failed to create secure login URL: %w", err)
	}

	kc := kiteconnect.New(m.apiKey)
	redirectParams := url.QueryEscape(signedParams)
	loginURL := kc.GetLoginURL() + "&redirect_params=" + redirectParams

	m.Logger.Info("Generated Kite login URL for session", "session_id", sessionID)
	return loginURL, nil
}

func (m *Manager) Shutdown() {
	m.Logger.Info("Shutting down Kite manager...")
	m.sessionManager.StopCleanupRoutine()
	if m.Instruments != nil {
		m.Instruments.Shutdown()
	}
	m.Logger.Info("Kite manager shutdown complete")
}

// SessionManager returns the underlying session manager.
func (m *Manager) SessionManager() *SessionManager {
	return m.sessionManager
}

// SessionSigner returns the underlying session signer.
func (m *Manager) SessionSigner() *SessionSigner {
	return m.sessionSigner
}

// RenderSuccessTemplate renders the success page template.
func (m *Manager) RenderSuccessTemplate(w http.ResponseWriter) error {
	templ, ok := m.templates[indexTemplate]
	if !ok {
		return errors.New("template not found")
	}
	return templ.ExecuteTemplate(w, "base", struct{ Title string }{"Login Successful"})
}

func setupTemplates() (map[string]*template.Template, error) {
	out := make(map[string]*template.Template)
	templateList := []string{indexTemplate}
	for _, templateName := range templateList {
		templ, err := template.ParseFS(templates.FS, "base.html", templateName)
		if err != nil {
			return out, fmt.Errorf("error parsing %s: %w", templateName, err)
		}
		out[templateName] = templ
	}
	return out, nil
}
