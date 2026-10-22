package kc

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
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
	KiteBaseURI string // Override Kite Connect API base URL (for testing)
	// SessionDuration is the backing session lifetime. It must be at least as
	// long as the OAuth access-token lifetime.
	SessionDuration time.Duration
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
		apiKey:          cfg.APIKey,
		apiSecret:       cfg.APISecret,
		kiteBaseURI:     cfg.KiteBaseURI,
		sessionDuration: cfg.SessionDuration,
		Logger:          cfg.Logger,
		metrics:         cfg.Metrics,
		Instruments:     cfg.Instruments,
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
	AccessToken  string
	RefreshToken string
	UserID       string
	ExpiresAt    time.Time
	generation   uint64
}

const (
	indexTemplate     = "login_success.html"
	authorizeTemplate = "authorize_interstitial.html"
)

// Manager orchestrates Kite Connect interactions and session management.
type Manager struct {
	apiKey          string
	apiSecret       string
	kiteBaseURI     string // Override for Kite Connect API base URL (testing)
	sessionDuration time.Duration
	// Bounded striped locks serialize renewal without retaining per-session lock entries.
	refreshLocks   [64]sync.Mutex
	Logger         *slog.Logger
	metrics        *metrics.Manager
	templates      map[string]*template.Template
	sessionManager *SessionManager
	sessionSigner  *SessionSigner
	Instruments    *instruments.Manager
}

// Metrics returns the metrics manager instance
func (m *Manager) Metrics() *metrics.Manager {
	return m.metrics
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
	sessionManager := NewSessionManagerWithDuration(m.Logger, m.sessionDuration)
	sessionManager.AddCleanupHook(func(s *Session) {
		m.Logger.Info("Cleaning up session", "session_id", s.ID)
	})
	sessionManager.StartCleanupRoutine(context.Background())
	m.sessionManager = sessionManager
}

// GetAuthenticatedClient is the single entry point for tools to get a valid Kite client.
func (m *Manager) GetAuthenticatedClient(sessionID string) (*kiteconnect.Client, error) {
	return m.GetAuthenticatedClientContext(context.Background(), sessionID)
}

// GetAuthenticatedClientContext binds every HTTP request made by this private
// client to the tool call's lifetime, including time spent waiting for approval.
func (m *Manager) GetAuthenticatedClientContext(ctx context.Context, sessionID string) (*kiteconnect.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	creds, err := m.authenticatedCredentials(sessionID)
	if err != nil {
		return nil, err
	}
	client := m.newKiteClient()
	client.SetAccessToken(creds.AccessToken)
	client.SetHTTPClient(newKiteHTTPClient(&sessionTransport{manager: m, sessionID: sessionID, base: http.DefaultTransport, callContext: ctx}))
	return client, nil
}

func (m *Manager) authenticatedCredentials(sessionID string) (*KiteCredentials, error) {
	if sessionID == "" {
		return nil, errors.New("invalid session ID")
	}

	// Re-read credentials after acquiring the lock: another request may already
	// have renewed them. Network calls never hold the session store mutex.
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(sessionID))
	lock := &m.refreshLocks[hash.Sum32()%uint32(len(m.refreshLocks))]
	lock.Lock()
	defer lock.Unlock()

	session, err := m.sessionManager.Get(sessionID)
	if err != nil || session.Terminated || session.Credentials == nil {
		return nil, errors.New("not logged into Kite. Please authenticate via the OAuth flow")
	}
	if !time.Now().Before(session.ExpiresAt) {
		return nil, errors.New("session has expired. Please re-authenticate via the OAuth flow")
	}
	creds := session.Credentials
	if !time.Now().Before(creds.ExpiresAt) {
		if session.refreshFailure != nil && time.Now().Before(session.refreshRetryAt) {
			return nil, session.refreshFailure
		}
		if creds.RefreshToken == "" {
			return nil, errors.New("kite session has expired. Please re-authenticate via the OAuth flow")
		}
		renewed, err := m.newKiteClient().RenewAccessToken(creds.RefreshToken, m.apiSecret)
		if err != nil {
			var apiErr kiteconnect.Error
			if errors.As(err, &apiErr) && apiErr.ErrorType == kiteconnect.TokenError {
				// Discard a permanently rejected grant. Temporary upstream failures leave
				// the refresh token intact so a later request can try again.
				_, _ = m.sessionManager.Terminate(sessionID)
				return nil, errors.New("kite refresh token is invalid or expired. Please re-authenticate via the OAuth flow")
			}
			err := errors.New("unable to refresh Kite access right now. Please try again later")
			m.sessionManager.recordRefreshFailure(sessionID, err)
			return nil, err
		}
		if renewed.AccessToken == "" || (renewed.UserID != "" && renewed.UserID != creds.UserID) {
			err := errors.New("invalid Kite token renewal response")
			m.sessionManager.recordRefreshFailure(sessionID, err)
			return nil, err
		}
		creds.AccessToken = renewed.AccessToken
		if renewed.RefreshToken != "" {
			creds.RefreshToken = renewed.RefreshToken
		}
		creds.ExpiresAt = kiteAccessTokenExpiry(time.Now())
		// This lookup-only update rejects logout/expiry during renewal and keeps
		// the original grant deadline. A refresh must never recreate a session.
		if err := m.sessionManager.UpdateCredentials(sessionID, creds); err != nil {
			return nil, errors.New("session ended during token renewal. Please re-authenticate via the OAuth flow")
		}
		updated, err := m.sessionManager.Get(sessionID)
		if err != nil {
			return nil, errors.New("session ended during token renewal. Please re-authenticate via the OAuth flow")
		}
		creds = updated.Credentials
	}
	return creds, nil
}

// SessionExpiry validates the application grant independently of the shorter
// Kite credential lifetime. OAuth uses it for issuance and every HTTP request.
func (m *Manager) SessionExpiry(sessionID string) (time.Time, error) {
	session, err := m.sessionManager.Get(sessionID)
	if err != nil || session.Terminated || session.Credentials == nil || !time.Now().Before(session.ExpiresAt) {
		return time.Time{}, errors.New("invalid or expired application grant")
	}
	return session.ExpiresAt, nil
}

func (m *Manager) newKiteClient() *kiteconnect.Client {
	client := kiteconnect.New(m.apiKey)
	client.SetHTTPClient(newKiteHTTPClient(nil))
	client.SetAppName("kite-mcp")
	if m.kiteBaseURI != "" {
		client.SetBaseURI(m.kiteBaseURI)
	}
	return client
}

// Kite access tokens expire at the next 06:00 IST cutoff, independently of
// the application grant. IST has no daylight-saving transitions.
func kiteAccessTokenExpiry(now time.Time) time.Time {
	local := now.In(time.FixedZone("IST", 5*60*60+30*60))
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), 6, 0, 0, 0, local.Location())
	if !local.Before(cutoff) {
		cutoff = cutoff.AddDate(0, 0, 1)
	}
	return cutoff
}

// CompleteLogin exchanges a request token for a new set of Kite credentials.
func (m *Manager) CompleteLogin(requestToken string) (*KiteCredentials, error) {
	kc := m.newKiteClient()
	userSess, err := kc.GenerateSession(requestToken, m.apiSecret)
	if err != nil {
		m.Logger.Error("Failed to generate Kite session from request token", "error", err)
		return nil, fmt.Errorf("failed to generate Kite session: %w", err)
	}

	creds := &KiteCredentials{
		AccessToken:  userSess.AccessToken,
		RefreshToken: userSess.RefreshToken,
		UserID:       userSess.UserID,
		ExpiresAt:    kiteAccessTokenExpiry(time.Now()),
	}

	if m.metrics != nil {
		m.metrics.TrackDailyUser(creds.UserID)
		m.metrics.Increment("user_logins")
	}

	m.Logger.Info("Successfully completed login and created new Kite credentials", "user_id", creds.UserID)
	return creds, nil
}

// GenerateLoginURL creates a login URL tied to a specific session ID.
func (m *Manager) GenerateLoginURL(sessionID string, state ...string) (string, error) {
	signedParams, err := m.sessionSigner.SignRedirectParams(sessionID)
	if err != nil {
		m.Logger.Error("Failed to sign redirect params for session", "session_id", sessionID, "error", err)
		return "", fmt.Errorf("failed to create secure login URL: %w", err)
	}

	kc := kiteconnect.New(m.apiKey)
	kc.SetAppName("kite-mcp")
	// The OAuth state is generated only after client consent. Kite returns
	// redirect_params unchanged to our callback alongside its request token.
	if len(state) > 0 {
		params, err := url.ParseQuery(signedParams)
		if err != nil {
			return "", err
		}
		params.Set("state", state[0])
		signedParams = params.Encode()
	}
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

// RenderClientConsent identifies the client and callback before starting Kite login.
func (m *Manager) RenderClientConsent(w http.ResponseWriter, clientName, redirectURI, flow, csrf string) error {
	templ, ok := m.templates[authorizeTemplate]
	if !ok {
		return errors.New("template not found")
	}
	return templ.ExecuteTemplate(w, "base", struct{ Title, ClientName, RedirectURI, Flow, CSRF string }{
		"Authorize MCP client", clientName, redirectURI, flow, csrf,
	})
}

func setupTemplates() (map[string]*template.Template, error) {
	out := make(map[string]*template.Template)
	templateList := []string{indexTemplate, authorizeTemplate}
	for _, templateName := range templateList {
		templ, err := template.ParseFS(templates.FS, "base.html", "brand_assets.html", templateName)
		if err != nil {
			return out, fmt.Errorf("error parsing %s: %w", templateName, err)
		}
		out[templateName] = templ
	}
	return out, nil
}
