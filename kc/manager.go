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
	APIKey             string                    // required
	APISecret          string                    // required
	Logger             *slog.Logger              // required
	InstrumentsConfig  *instruments.UpdateConfig // optional - defaults to instruments.DefaultUpdateConfig()
	InstrumentsManager *instruments.Manager      // optional - if provided, skips creating new instruments manager
	SessionSigner      *SessionSigner            // optional - if nil, creates new session signer
	Metrics            *metrics.Manager          // optional - for tracking user metrics
}

// New creates a new kc Manager with the given configuration
func New(cfg Config) (*Manager, error) {
	// Validate required fields
	if cfg.APIKey == "" {
		return nil, errors.New("APIKey is required")
	}
	if cfg.APISecret == "" {
		return nil, errors.New("APISecret is required")
	}
	if cfg.Logger == nil {
		return nil, errors.New("logger is required") // TODO: maybe create a default logger later on
	}

	// Create or use provided instruments manager
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
	// Add fields here
	Client *kiteconnect.Client // TODO: this can be made private ?
}

// NewKiteConnect creates a new KiteConnect instance
func NewKiteConnect(apiKey string) *KiteConnect {
	client := kiteconnect.New(apiKey)

	return &KiteConnect{
		Client: client,
	}
}

const (
	// Template names
	indexTemplate = "login_success.html"

	// HTTP error messages
	missingParamsMessage  = "missing MCP session_id or Kite request_token"
	sessionErrorMessage   = "error completing Kite session"
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
	apiKey    string
	apiSecret string
	Logger    *slog.Logger
	metrics   *metrics.Manager

	templates map[string]*template.Template

	Instruments    *instruments.Manager
	sessionManager *SessionRegistry
	sessionSigner  *SessionSigner
}

// NewManager creates a new manager with default configuration
// Deprecated: Use New(Config{APIKey: apiKey, APISecret: apiSecret, Logger: logger}) instead
func NewManager(apiKey, apiSecret string, logger *slog.Logger) (*Manager, error) {
	return New(Config{
		APIKey:    apiKey,
		APISecret: apiSecret,
		Logger:    logger,
	})
}

// initializeTemplates sets up HTML templates
func (m *Manager) initializeTemplates() error {
	templates, err := setupTemplates()
	if err != nil {
		return fmt.Errorf("failed to setup templates: %w", err)
	}
	m.templates = templates
	return nil
}

// initializeSessionSigner sets up HMAC session signing
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

// initializeSessionManager sets up the session manager with cleanup hooks
// initializeSessionManager creates and configures the session manager
func (m *Manager) initializeSessionManager() {
	sessionManager := NewSessionRegistry(m.Logger)

	// Add cleanup hook for Kite sessions
	sessionManager.AddCleanupHook(m.kiteSessionCleanupHook)

	// Start cleanup routine
	sessionManager.StartCleanupRoutine(context.Background())

	m.sessionManager = sessionManager
}

// kiteSessionCleanupHook handles cleanup of Kite sessions
func (m *Manager) kiteSessionCleanupHook(session *MCPSession) {
	if kiteData, ok := session.Data.(*KiteSessionData); ok && kiteData != nil && kiteData.Kite != nil {
		m.Logger.Info("Cleaning up Kite session for MCP session ID", "session_id", session.ID)
		_, _ = kiteData.Kite.Client.InvalidateAccessToken()
	}
}

// validateSessionID checks if a session ID is empty and returns appropriate error
func (m *Manager) validateSessionID(sessionID string) error {
	if sessionID == "" {
		return ErrInvalidSessionID
	}
	return nil
}

// createKiteSessionData creates new KiteSessionData for a session
func (m *Manager) createKiteSessionData(sessionID string) *KiteSessionData {
	m.Logger.Info("Creating new Kite session data for MCP session ID", "session_id", sessionID)
	return &KiteSessionData{
		Kite: NewKiteConnect(m.apiKey),
	}
}

// extractKiteSessionData safely extracts KiteSessionData from interface{}
func (m *Manager) extractKiteSessionData(data any, sessionID string) (*KiteSessionData, error) {
	kiteData, ok := data.(*KiteSessionData)
	if !ok || kiteData == nil {
		m.Logger.Warn("Invalid Kite data type for MCP session ID", "session_id", sessionID)
		return nil, ErrSessionNotFound
	}
	return kiteData, nil
}

// logSessionCreated logs when a new session is created
func (m *Manager) logSessionCreated(sessionID string) {
	m.Logger.Info("Successfully created new Kite data for MCP session ID", "session_id", sessionID)
}

// logSessionRetrieved logs when an existing session is retrieved
func (m *Manager) logSessionRetrieved(sessionID string) {
	m.Logger.Info("Successfully retrieved existing Kite data for MCP session ID", "session_id", sessionID)
}

// logSessionRetrievedData logs when session data is successfully retrieved
func (m *Manager) logSessionRetrievedData(sessionID string) {
	m.Logger.Info("Successfully retrieved Kite data for MCP session ID", "session_id", sessionID)
}

// GetOrCreateSession retrieves an existing Kite session or creates a new one atomically
func (m *Manager) GetOrCreateSession(mcpSessionID string) (*KiteSessionData, bool, error) {
	if err := m.validateSessionID(mcpSessionID); err != nil {
		m.Logger.Warn("GetOrCreateSession called with empty MCP session ID")
		return nil, false, err
	}

	// Use atomic GetOrCreateSessionData to eliminate TOCTOU race condition
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

	// Validate session first
	if err := m.validateSession(mcpSessionID); err != nil {
		m.Logger.Error("MCP session validation failed", "error", err)
		return nil, err
	}

	m.Logger.Debug("Getting Kite data for MCP session ID", "session_id", mcpSessionID)
	data, err := m.sessionManager.GetSessionData(mcpSessionID)
	if err != nil {
		m.Logger.Error("Failed to get Kite data", "error", err)
		return nil, ErrSessionNotFound
	}

	kiteData, err := m.extractKiteSessionData(data, mcpSessionID)
	if err != nil {
		return nil, err
	}

	m.logSessionRetrievedData(mcpSessionID)
	return kiteData, nil
}

// validateSession checks if a MCP session is valid and active
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

func (m *Manager) ClearSession(sessionID string) {
	if err := m.validateSessionID(sessionID); err != nil {
		return
	}

	// Terminate the session, which will trigger cleanup hooks
	if _, err := m.sessionManager.Terminate(sessionID); err != nil {
		m.Logger.Error("Error terminating session", "session_id", sessionID, "error", err)
	} else {
		m.Logger.Info("Cleaning up Kite session for MCP session ID", "session_id", sessionID)
	}
}

// ClearSessionData clears the session data without terminating the session
func (m *Manager) ClearSessionData(sessionID string) error {
	if err := m.validateSessionID(sessionID); err != nil {
		return err
	}

	// Get the session to perform cleanup on the data
	session, err := m.sessionManager.GetSession(sessionID)
	if err != nil {
		m.Logger.Error("Failed to get session for data cleanup", "error", err)
		return err
	}

	// Cleanup the Kite session data if it exists
	if session.Data != nil {
		m.kiteSessionCleanupHook(session)
	}

	// Clear the session data without terminating the session
	if err := m.sessionManager.UpdateSessionData(sessionID, nil); err != nil {
		m.Logger.Error("Error clearing session data", "session_id", sessionID, "error", err)
		return err
	}

	m.Logger.Info("Cleared session data for MCP session ID", "session_id", sessionID)
	return nil
}

// GenerateSession creates a new MCP session with Kite data and returns the session ID
func (m *Manager) GenerateSession() string {
	m.Logger.Info("Generating new MCP session with Kite data")

	sessionID := m.sessionManager.GenerateWithData(m.createKiteSessionData(""))
	m.Logger.Info("Generated new MCP session with ID", "session_id", sessionID)

	return sessionID
}

// No longer needed - replaced by GetOrCreateSession

func (m *Manager) SessionLoginURL(mcpSessionID string) (string, error) {
	if err := m.validateSessionID(mcpSessionID); err != nil {
		m.Logger.Warn("SessionLoginURL called with empty MCP session ID")
		return "", err
	}

	m.Logger.Debug("Retrieving or creating Kite data for MCP session ID", "session_id", mcpSessionID)
	// Use GetOrCreateSession instead of GetSession to automatically create a session if needed
	kiteData, isNew, err := m.GetOrCreateSession(mcpSessionID)
	if err != nil {
		m.Logger.Error("Failed to get or create Kite data", "error", err)
		return "", err
	}

	if isNew {
		m.Logger.Info("Created new Kite session for MCP session ID", "session_id", mcpSessionID)
	}

	// Create signed redirect parameters for security
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

	m.Logger.Info("Completing Kite auth for MCP session", "session_id", mcpSessionID, "request_token", kiteRequestToken)

	kiteData, err := m.GetSession(mcpSessionID)
	if err != nil {
		m.Logger.Error("Failed to complete session", "session_id", mcpSessionID, "error", err)
		return ErrSessionNotFound
	}

	m.Logger.Debug("Generating Kite session with request token")
	userSess, err := kiteData.Kite.Client.GenerateSession(kiteRequestToken, m.apiSecret)
	if err != nil {
		m.Logger.Error("Failed to generate Kite session", "error", err)
		return fmt.Errorf("failed to generate Kite session: %w", err)
	}

	m.Logger.Info("Setting Kite access token for MCP session", "session_id", mcpSessionID)
	kiteData.Kite.Client.SetAccessToken(userSess.AccessToken)

	// Compliance log for successful login
	m.Logger.Info("COMPLIANCE: User login completed successfully",
		"event", "user_login_success",
		"user_id", userSess.UserID,
		"session_id", mcpSessionID,
		"timestamp", time.Now().UTC().Format(time.RFC3339),
		"user_name", userSess.UserName,
		"user_type", userSess.UserType,
	)

	// Track successful login
	if m.metrics != nil {
		m.metrics.TrackDailyUser(userSess.UserID)
		m.metrics.Increment("user_logins")
	}

	return nil
}

// Session management utility methods

// GetActiveSessionCount returns the number of active sessions
func (m *Manager) GetActiveSessionCount() int {
	return len(m.sessionManager.ListActiveSessions())
}

// Session extension has been removed to enforce fixed session durations

// CleanupExpiredSessions manually triggers cleanup of expired MCP sessions
func (m *Manager) CleanupExpiredSessions() int {
	return m.sessionManager.CleanupExpiredSessions()
}

// StopCleanupRoutine stops the background cleanup routine
func (m *Manager) StopCleanupRoutine() {
	m.sessionManager.StopCleanupRoutine()
}

// HasMetrics returns true if metrics manager is available
func (m *Manager) HasMetrics() bool {
	return m.metrics != nil
}

// IncrementMetric increments a metric counter by 1
func (m *Manager) IncrementMetric(key string) {
	if m.metrics != nil {
		m.metrics.Increment(key)
	}
}

// IncrementDailyMetric increments a daily metric counter by 1
func (m *Manager) IncrementDailyMetric(key string) {
	if m.metrics != nil {
		m.metrics.IncrementDaily(key)
	}
}

// IncrementDailyMetricWithLabels increments a daily metric counter with labels by 1
func (m *Manager) IncrementDailyMetricWithLabels(key string, labels map[string]string) {
	if m.metrics != nil {
		m.metrics.IncrementDailyWithLabels(key, labels)
	}
}

// Shutdown gracefully shuts down the manager and all its components
func (m *Manager) Shutdown() {
	m.Logger.Info("Shutting down Kite manager...")

	// Stop session cleanup routines
	m.sessionManager.StopCleanupRoutine()

	// Shutdown metrics manager (stops cleanup routine)
	if m.metrics != nil {
		m.metrics.Shutdown()
	}

	// Shutdown instruments manager (stops scheduler)
	m.Instruments.Shutdown()

	m.Logger.Info("Kite manager shutdown complete")
}

// GetInstrumentsStats returns current instruments update statistics
func (m *Manager) GetInstrumentsStats() instruments.UpdateStats {
	return m.Instruments.GetUpdateStats()
}

// UpdateInstrumentsConfig updates the instruments manager configuration
func (m *Manager) UpdateInstrumentsConfig(config *instruments.UpdateConfig) {
	m.Instruments.UpdateConfig(config)
}

// ForceInstrumentsUpdate forces an immediate instruments update
func (m *Manager) ForceInstrumentsUpdate() error {
	return m.Instruments.ForceUpdateInstruments()
}

// SessionManager returns the MCP session manager instance
func (m *Manager) SessionManager() *SessionRegistry {
	return m.sessionManager
}

// SessionSigner returns the session signer instance
func (m *Manager) SessionSigner() *SessionSigner {
	return m.sessionSigner
}

// UpdateSessionSignerExpiry updates the signature expiry duration
func (m *Manager) UpdateSessionSignerExpiry(duration time.Duration) {
	m.sessionSigner.SetSignatureExpiry(duration)
}

func setupTemplates() (map[string]*template.Template, error) {
	out := map[string]*template.Template{}

	templateList := []string{indexTemplate}

	for _, templateName := range templateList {
		// Parse template with base template for composition support
		templ, err := template.ParseFS(templates.FS, "base.html", templateName)
		if err != nil {
			return out, fmt.Errorf("error parsing %s: %w", templateName, err)
		}
		out[templateName] = templ
	}

	return out, nil
}

// handleCallbackError handles error responses for callback processing
func (m *Manager) handleCallbackError(w http.ResponseWriter, message string, statusCode int, logMessage string, args ...any) {
	m.Logger.Error(logMessage, args...)
	http.Error(w, message, statusCode)
}

// HandleKiteCallback returns an HTTP handler for Kite authentication callbacks
func (m *Manager) HandleKiteCallback() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		m.Logger.Info("Received Kite callback request", "url", r.URL.String())
		requestToken, mcpSessionID, err := m.extractCallbackParams(r)
		if err != nil {
			m.handleCallbackError(w, missingParamsMessage, http.StatusBadRequest, "Invalid callback parameters", "error", err)
			return
		}

		m.Logger.Info("Processing Kite callback for MCP session ID", "session_id", mcpSessionID, "request_token", requestToken)

		if err := m.CompleteSession(mcpSessionID, requestToken); err != nil {
			m.handleCallbackError(w, sessionErrorMessage, http.StatusInternalServerError, "Error completing Kite session for MCP session %s: %v", mcpSessionID, err)
			return
		}

		m.Logger.Info("Kite session completed successfully", "session_id", mcpSessionID)

		if err := m.renderSuccessTemplate(w); err != nil {
			m.Logger.Error("Template failed to load - this is a fatal error", "error", err)
			http.Error(w, "Internal server error: template not available", http.StatusInternalServerError)
			return
		}

		m.Logger.Info("Kite callback completed successfully", "session_id", mcpSessionID)
	}
}

// extractCallbackParams extracts and validates callback parameters with signature verification
func (m *Manager) extractCallbackParams(r *http.Request) (kiteRequestToken, mcpSessionID string, err error) {
	qVals := r.URL.Query()
	kiteRequestToken = qVals.Get("request_token")
	signedSessionID := qVals.Get("session_id")

	if signedSessionID == "" || kiteRequestToken == "" {
		return "", "", errors.New("missing required parameters (MCP session_id or Kite request_token)")
	}

	// Verify the signed session ID
	mcpSessionID, err = m.sessionSigner.VerifySessionID(signedSessionID)
	if err != nil {
		m.Logger.Error("Failed to verify session signature", "error", err)
		return "", "", fmt.Errorf("invalid or tampered session parameter: %w", err)
	}

	return kiteRequestToken, mcpSessionID, nil
}

// TemplateData holds data for template rendering
type TemplateData struct {
	Title string
}

// renderSuccessTemplate renders the success page template
func (m *Manager) renderSuccessTemplate(w http.ResponseWriter) error {
	templ, ok := m.templates[indexTemplate]
	if !ok {
		return errors.New(templateNotFoundError)
	}

	data := TemplateData{
		Title: "Login Successful",
	}

	return templ.ExecuteTemplate(w, "base", data)
}
