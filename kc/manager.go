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
	sessionManager *SessionRegistry
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
	sessionManager := NewSessionRegistry(m.Logger)
	sessionManager.AddCleanupHook(m.kiteSessionCleanupHook)
	sessionManager.StartCleanupRoutine(context.Background())
	m.sessionManager = sessionManager
}

func (m *Manager) kiteSessionCleanupHook(session *MCPSession) {
	if kiteData, ok := session.Data.(*KiteSessionData); ok && kiteData != nil && kiteData.Kite != nil {
		m.Logger.Info("Cleaning up Kite session for MCP session ID", "session_id", session.ID)
		_, _ = kiteData.Kite.Client.InvalidateAccessToken()
	}
}

func (m *Manager) validateSessionID(sessionID string) error {
	if sessionID == "" {
		return ErrInvalidSessionID
	}
	return nil
}

func (m *Manager) createKiteSessionData(sessionID string) *KiteSessionData {
	m.Logger.Info("Creating new Kite session data for MCP session ID", "session_id", sessionID)
	return &KiteSessionData{
		Kite: NewKiteConnect(m.apiKey),
	}
}

func (m *Manager) extractKiteSessionData(data any, sessionID string) (*KiteSessionData, error) {
	kiteData, ok := data.(*KiteSessionData)
	if !ok || kiteData == nil {
		m.Logger.Warn("Invalid Kite data type for MCP session ID", "session_id", sessionID)
		return nil, ErrSessionNotFound
	}
	return kiteData, nil
}

func (m *Manager) logSessionCreated(sessionID string) {
	m.Logger.Info("Successfully created new Kite data for MCP session ID", "session_id", sessionID)
}

func (m *Manager) logSessionRetrieved(sessionID string) {
	m.Logger.Info("Successfully retrieved existing Kite data for MCP session ID", "session_id", sessionID)
}

func (m *Manager) GetOrCreateSession(mcpSessionID string) (*KiteSessionData, bool, error) {
	if err := m.validateSessionID(mcpSessionID); err != nil {
		m.Logger.Warn("GetOrCreateSession called with empty MCP session ID")
		return nil, false, err
	}
	data, isNew, err := m.sessionManager.GetOrCreateSessionData(mcpSessionID, func() any {
		return m.createKiteSessionData(mcpSessionID)
	})
	if err != nil {
		m.Logger.Error("Failed to get or create session data", "error", err)
		return nil, false, ErrSessionNotFound
	}
	kiteData, err := m.extractKiteSessionData(data, mcpSessionID)
	if err != nil {
		return nil, false, err
	}
	if isNew {
		m.logSessionCreated(mcpSessionID)
	} else {
		m.logSessionRetrieved(mcpSessionID)
	}
	return kiteData, isNew, nil
}

func (m *Manager) GetSession(mcpSessionID string) (*KiteSessionData, error) {
	if err := m.validateSessionID(mcpSessionID); err != nil {
		m.Logger.Warn("GetSession called with empty MCP session ID")
		return nil, ErrSessionNotFound
	}
	if err := m.validateSession(mcpSessionID); err != nil {
		m.Logger.Error("MCP session validation failed", "error", err)
		return nil, err
	}
	data, err := m.sessionManager.GetSessionData(mcpSessionID)
	if err != nil {
		m.Logger.Error("Failed to get Kite data", "error", err)
		return nil, ErrSessionNotFound
	}
	kiteData, err := m.extractKiteSessionData(data, mcpSessionID)
	if err != nil {
		return nil, err
	}
	m.Logger.Info("Successfully retrieved Kite data for MCP session ID", "session_id", mcpSessionID)
	return kiteData, nil
}

func (m *Manager) validateSession(sessionID string) error {
	isTerminated, err := m.sessionManager.Validate(sessionID)
	if err != nil {
		m.Logger.Error("MCP session validation failed", "session_id", sessionID, "error", err)
		return ErrSessionNotFound
	}
	if isTerminated {
		m.Logger.Warn("MCP session is terminated", "session_id", sessionID)
		return ErrSessionNotFound
	}
	return nil
}

// ClearSession terminates the session and runs cleanup hooks.
func (m *Manager) ClearSession(sessionID string) {
	if err := m.validateSessionID(sessionID); err != nil {
		return
	}
	if _, err := m.sessionManager.Terminate(sessionID); err != nil {
		m.Logger.Error("Error terminating session", "session_id", sessionID, "error", err)
	} else {
		m.Logger.Info("Cleaning up Kite session for MCP session ID", "session_id", sessionID)
	}
}

// ClearSessionData clears the session data without terminating the session.
func (m *Manager) ClearSessionData(sessionID string) error {
	if err := m.validateSessionID(sessionID); err != nil {
		return err
	}
	session, err := m.sessionManager.GetSession(sessionID)
	if err != nil {
		m.Logger.Error("Failed to get session for data cleanup", "error", err)
		return err
	}
	if session.Data != nil {
		m.kiteSessionCleanupHook(session)
	}
	if err := m.sessionManager.UpdateSessionData(sessionID, nil); err != nil {
		m.Logger.Error("Error clearing session data", "session_id", sessionID, "error", err)
		return err
	}
	m.Logger.Info("Cleared session data for MCP session ID", "session_id", sessionID)
	return nil
}

// CleanupExpiredSessions manually triggers cleanup of expired MCP sessions.
func (m *Manager) CleanupExpiredSessions() int {
	return m.sessionManager.CleanupExpiredSessions()
}

func (m *Manager) GenerateSession() string {
	m.Logger.Info("Generating new MCP session with Kite data")
	sessionID := m.sessionManager.GenerateWithData(m.createKiteSessionData(""))
	m.Logger.Info("Generated new MCP session with ID", "session_id", sessionID)
	return sessionID
}

func (m *Manager) SessionLoginURL(mcpSessionID string) (string, error) {
	if err := m.validateSessionID(mcpSessionID); err != nil {
		m.Logger.Warn("SessionLoginURL called with empty MCP session ID")
		return "", err
	}
	kiteData, isNew, err := m.GetOrCreateSession(mcpSessionID)
	if err != nil {
		m.Logger.Error("Failed to get or create Kite data", "error", err)
		return "", err
	}
	if isNew {
		m.Logger.Info("Created new Kite session for MCP session ID", "session_id", mcpSessionID)
	}
	signedParams, err := m.sessionSigner.SignRedirectParams(mcpSessionID)
	if err != nil {
		m.Logger.Error("Failed to sign redirect params for session", "session_id", mcpSessionID, "error", err)
		return "", fmt.Errorf("failed to create secure login URL: %w", err)
	}
	redirectParams := url.QueryEscape(signedParams)
	loginURL := kiteData.Kite.Client.GetLoginURL() + "&redirect_params=" + redirectParams
	m.Logger.Info("Generated Kite login URL for MCP session", "session_id", mcpSessionID)
	return loginURL, nil
}

func (m *Manager) CompleteSession(mcpSessionID, kiteRequestToken string) error {
	if err := m.validateSessionID(mcpSessionID); err != nil {
		m.Logger.Warn("CompleteSession called with empty MCP session ID")
		return err
	}
	m.Logger.Info("Completing Kite auth for MCP session", "session_id", mcpSessionID)
	kiteData, err := m.GetSession(mcpSessionID)
	if err != nil {
		m.Logger.Error("Failed to complete session", "session_id", mcpSessionID, "error", err)
		return ErrSessionNotFound
	}
	userSess, err := kiteData.Kite.Client.GenerateSession(kiteRequestToken, m.apiSecret)
	if err != nil {
		m.Logger.Error("Failed to generate Kite session", "error", err)
		return fmt.Errorf("failed to generate Kite session: %w", err)
	}
	m.Logger.Info("Setting Kite access token for MCP session", "session_id", mcpSessionID)
	kiteData.Kite.Client.SetAccessToken(userSess.AccessToken)
	if m.metrics != nil {
		m.metrics.TrackDailyUser(userSess.UserID)
		m.metrics.Increment("user_logins")
	}
	return nil
}

func (m *Manager) GetActiveSessionCount() int {
	return len(m.sessionManager.ListActiveSessions())
}

func (m *Manager) Shutdown() {
	m.Logger.Info("Shutting down Kite manager...")
	m.sessionManager.StopCleanupRoutine()
	if m.metrics != nil {
		m.metrics.Shutdown()
	}
	m.Instruments.Shutdown()
	m.Logger.Info("Kite manager shutdown complete")
}

func (m *Manager) SessionManager() *SessionRegistry {
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
