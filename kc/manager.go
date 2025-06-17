package kc

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/kite-mcp-server/app/metrics"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
	"github.com/zerodha/kite-mcp-server/kc/templates"
)

// Config holds configuration for creating a new kc Manager
type Config struct {
	APIKey             string
	APISecret          string
	Logger             *slog.Logger
	InstrumentsConfig  *instruments.UpdateConfig
	InstrumentsManager *instruments.Manager
	SessionSigner      *SessionSigner
	Metrics            *metrics.Manager
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

	var instrumentsManager *instruments.Manager
	if cfg.InstrumentsManager != nil {
		instrumentsManager = cfg.InstrumentsManager
	} else {
		var err error
		instrumentsManager, err = instruments.New(instruments.Config{
			UpdateConfig: cfg.InstrumentsConfig,
			Logger:       cfg.Logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create instruments manager: %w", err)
		}
	}

	m := &Manager{
		apiKey:    cfg.APIKey,
		apiSecret: cfg.APISecret,
		Logger:    cfg.Logger,
		metrics:   cfg.Metrics,
	}

	if err := m.initializeTemplates(); err != nil {
		return nil, fmt.Errorf("failed to initialize Kite manager: %w", err)
	}

	if err := m.initializeSessionSigner(cfg.SessionSigner); err != nil {
		return nil, fmt.Errorf("failed to initialize session signer: %w", err)
	}

	m.Instruments = instrumentsManager
	m.initializeSessionManager()

	return m, nil
}

// KiteConnect wraps the Kite Connect client
type KiteConnect struct {
	Client *kiteconnect.Client
}

// NewKiteConnect creates a new KiteConnect instance
func NewKiteConnect(apiKey string) *KiteConnect {
	client := kiteconnect.New(apiKey)
	return &KiteConnect{
		Client: client,
	}
}

const (
	indexTemplate         = "login_success.html"
	templateNotFoundError = "template not found"
)

var (
	ErrSessionNotFound  = errors.New("MCP session not found or Kite session not associated, try to login again")
	ErrInvalidSessionID = errors.New("invalid MCP session ID, please try logging in again")
)

type KiteSessionData struct {
	Kite *KiteConnect
}

type Manager struct {
	apiKey         string
	apiSecret      string
	Logger         *slog.Logger
	metrics        *metrics.Manager
	templates      map[string]*template.Template
	Instruments    *instruments.Manager
	sessionManager *SessionManager
	sessionSigner  *SessionSigner
}

func (m *Manager) initializeTemplates() error {
	templates, err := setupTemplates()
	if err != nil {
		return fmt.Errorf("failed to setup templates: %w", err)
	}
	m.templates = templates
	return nil
}

func (m *Manager) initializeSessionSigner(customSigner *SessionSigner) error {
	if customSigner != nil {
		m.sessionSigner = customSigner
		return nil
	}
	signer, err := NewSessionSigner()
	if err != nil {
		return fmt.Errorf("failed to create session signer: %w", err)
	}
	m.sessionSigner = signer
	return nil
}

func (m *Manager) initializeSessionManager() {
	sessionManager := NewSessionManager(m.Logger)
	sessionManager.AddCleanupHook(m.kiteSessionCleanupHook)
	sessionManager.StartCleanupRoutine(context.Background())
	m.sessionManager = sessionManager
}

func (m *Manager) kiteSessionCleanupHook(session *Session) {
	if kiteData, ok := session.Data.(*KiteSessionData); ok && kiteData != nil && kiteData.Kite != nil {
		m.Logger.Info("Cleaning up Kite session for MCP session ID", "session_id", session.ID)
		_, _ = kiteData.Kite.Client.InvalidateAccessToken()
	}
}

func (m *Manager) createKiteSessionData(sessionID string) *KiteSessionData {
	m.Logger.Info("Creating new Kite session data for MCP session ID", "session_id", sessionID)
	return &KiteSessionData{
		Kite: NewKiteConnect(m.apiKey),
	}
}

// GetOrCreateSession gets an existing session or creates a new one for a given ID.
// This is the entry point for all tool requests.
func (m *Manager) GetOrCreateSession(mcpSessionID string) (*KiteSessionData, bool, error) {
	if mcpSessionID == "" {
		return nil, false, ErrInvalidSessionID
	}

	session, isNew, err := m.sessionManager.GetOrCreate(mcpSessionID)
	if err != nil {
		m.Logger.Error("Failed to get or create session", "session_id", mcpSessionID, "error", err)
		return nil, false, err
	}

	// If the session is new or the data is missing, populate it. This is safe
	// because GetOrCreate will have returned an error for an existing session
	// that is in use by another flow (e.g. OAuth).
	if session.Data == nil {
		m.Logger.Info("Populating new Kite data for session", "session_id", mcpSessionID)
		session.Data = m.createKiteSessionData(mcpSessionID)
	}

	// Now, type-assert the data. If it's not the type we expect, it's an
	// unrecoverable state error.
	kiteData, ok := session.Data.(*KiteSessionData)
	if !ok {
		m.Logger.Error("Session data has wrong type for a tool-based operation", "session_id", mcpSessionID)
		return nil, false, fmt.Errorf("session %s is in use by another flow", mcpSessionID)
	}

	return kiteData, isNew, nil
}

// GenerateSession is used by the non-OAuth login tool flow.
func (m *Manager) GenerateSession() string {
	m.Logger.Info("Generating new MCP session with pre-populated Kite data for legacy flow")
	// For this flow, we create the data immediately.
	sessionID := m.sessionManager.GenerateWithData(m.createKiteSessionData(""))
	m.Logger.Info("Generated new legacy session with ID", "session_id", sessionID)
	return sessionID
}

// GenerateOAuthSessionID generates a temporary, empty session for the OAuth redirect flow.
func (m *Manager) GenerateOAuthSessionID() string {
	m.Logger.Info("Generating temporary OAuth session ID")
	return m.sessionManager.GenerateWithData(nil)
}

// GetLoginToolURL ensures a persistent session exists and returns a login URL for it.
// This is used by the legacy LoginTool flow.
func (m *Manager) GetLoginToolURL(mcpSessionID string) (string, error) {
	// This flow requires a persistent session with KiteSessionData.
	// GetOrCreateSession ensures this is the case.
	_, _, err := m.GetOrCreateSession(mcpSessionID)
	if err != nil {
		return "", err
	}

	return m.generateKiteLoginURL(mcpSessionID)
}

// GenerateOAuthLoginURL returns a login URL for a temporary OAuth session ID
// without accessing or modifying the session data itself.
func (m *Manager) GenerateOAuthLoginURL(mcpSessionID string) (string, error) {
	// This flow must NOT touch the session data, as it contains the fosite.AuthorizeRequester.
	return m.generateKiteLoginURL(mcpSessionID)
}

// generateKiteLoginURL is a helper that performs the common URL creation logic.
func (m *Manager) generateKiteLoginURL(mcpSessionID string) (string, error) {
	signedParams, err := m.sessionSigner.SignRedirectParams(mcpSessionID)
	if err != nil {
		m.Logger.Error("Failed to sign redirect params for session", "session_id", mcpSessionID, "error", err)
		return "", fmt.Errorf("failed to create secure login URL: %w", err)
	}

	// Use a fresh KiteConnect client just for generating the URL.
	kc := NewKiteConnect(m.apiKey)
	redirectParams := url.QueryEscape(signedParams)
	loginURL := kc.Client.GetLoginURL() + "&redirect_params=" + redirectParams

	m.Logger.Info("Generated Kite login URL for session", "session_id", mcpSessionID)
	return loginURL, nil
}

// CompleteLogin exchanges a request token for a user session and populates the given KiteSessionData.
// It does not manage session state, only performs the API call.
func (m *Manager) CompleteLogin(ksd *KiteSessionData, kiteRequestToken string) (*kiteconnect.UserSession, error) {
	if ksd == nil || ksd.Kite == nil {
		return nil, errors.New("cannot complete login with nil KiteSessionData")
	}

	userSess, err := ksd.Kite.Client.GenerateSession(kiteRequestToken, m.apiSecret)
	if err != nil {
		m.Logger.Error("Failed to generate Kite session from request token", "error", err)
		return nil, fmt.Errorf("failed to generate Kite session: %w", err)
	}

	ksd.Kite.Client.SetAccessToken(userSess.AccessToken)

	if m.metrics != nil {
		m.metrics.TrackDailyUser(userSess.UserID)
		m.metrics.Increment("user_logins")
	}

	m.Logger.Info("Successfully completed login and set access token", "user_id", userSess.UserID)
	return &userSess, nil
}

// ClearSession terminates a session and runs cleanup hooks.
func (m *Manager) ClearSession(sessionID string) {
	if _, err := m.sessionManager.Terminate(sessionID); err != nil {
		m.Logger.Error("Error terminating session", "session_id", sessionID, "error", err)
	} else {
		m.Logger.Info("Terminated and cleaned up session", "session_id", sessionID)
	}
}

// ClearSessionData clears the session data without terminating the session.
func (m *Manager) ClearSessionData(sessionID string) error {
	session, err := m.sessionManager.Get(sessionID)
	if err != nil {
		return err
	}
	// Run cleanup hook on existing data before clearing.
	if session.Data != nil {
		m.kiteSessionCleanupHook(session)
	}
	if err := m.sessionManager.UpdateSessionData(sessionID, nil); err != nil {
		m.Logger.Error("Error clearing session data", "session_id", sessionID, "error", err)
		return err
	}
	m.Logger.Info("Cleared session data", "session_id", sessionID)
	return nil
}

// Shutdown gracefully shuts down the Kite manager.
func (m *Manager) Shutdown() {
	m.Logger.Info("Shutting down Kite manager...")
	m.sessionManager.StopCleanupRoutine()
	if m.metrics != nil {
		m.metrics.Shutdown()
	}
	m.Instruments.Shutdown()
	m.Logger.Info("Kite manager shutdown complete")
}

func (m *Manager) SessionManager() *SessionManager {
	return m.sessionManager
}

func (m *Manager) SessionSigner() *SessionSigner {
	return m.sessionSigner
}

// RenderSuccessTemplate renders the success page template.
func (m *Manager) RenderSuccessTemplate(w http.ResponseWriter) error {
	templ, ok := m.templates[indexTemplate]
	if !ok {
		return errors.New(templateNotFoundError)
	}
	data := struct {
		Title string
	}{
		Title: "Login Successful",
	}
	return templ.ExecuteTemplate(w, "base", data)
}

func setupTemplates() (map[string]*template.Template, error) {
	out := map[string]*template.Template{}
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
