package kc

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// Default session configuration
	// Keep the backing session alive for at least the default OAuth token
	// lifetime. Callers that configure longer-lived tokens should pass that
	// duration through NewSessionManagerWithDuration.
	DefaultSessionDuration = 24 * time.Hour
	DefaultCleanupInterval = 30 * time.Minute

	// Error messages
	errSessionNotFound        = "session ID not found"
	errCannotUpdateTerminated = "cannot update terminated session"
	mcpSessionPrefix          = "kitemcp-"
)

// Session represents a single, long-lived user/client session.
type Session struct {
	ID          string
	Terminated  bool
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Credentials *KiteCredentials // Holds the short-lived Kite credentials.
	OAuthData   any              // Holds temporary data for an in-progress OAuth flow.
}

// SessionManager manages all active sessions.
type SessionManager struct {
	sessions        map[string]*Session
	mu              sync.RWMutex
	sessionDuration time.Duration
	cleanupHooks    []CleanupHook
	cleanupContext  context.Context
	cleanupCancel   context.CancelFunc
	logger          *slog.Logger
}

// CleanupHook is called when a session is terminated or expires.
type CleanupHook func(session *Session)

// NewSessionManager creates a new manager for MCP sessions.
func NewSessionManager(logger *slog.Logger) *SessionManager {
	return NewSessionManagerWithDuration(logger, DefaultSessionDuration)
}

// NewSessionManagerWithDuration creates a session manager with the requested
// session lifetime. A non-positive duration uses DefaultSessionDuration.
func NewSessionManagerWithDuration(logger *slog.Logger, sessionDuration time.Duration) *SessionManager {
	if sessionDuration <= 0 {
		sessionDuration = DefaultSessionDuration
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &SessionManager{
		sessions:        make(map[string]*Session),
		sessionDuration: sessionDuration,
		cleanupHooks:    make([]CleanupHook, 0),
		cleanupContext:  ctx,
		cleanupCancel:   cancel,
		logger:          logger,
	}
}

// GenerateWithData creates a new session with a unique ID and optional initial data.
func (sm *SessionManager) GenerateWithData(initialData any) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sessionID := mcpSessionPrefix + uuid.New().String()
	now := time.Now()
	expiresAt := now.Add(sm.sessionDuration)

	sm.sessions[sessionID] = &Session{
		ID:        sessionID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		OAuthData: initialData,
	}

	sm.logger.Info("Generated new session", "session_id", sessionID, "expires_at", expiresAt)
	return sessionID
}

// Generate creates a new session and satisfies the server.SessionIdManager interface.
func (sm *SessionManager) Generate() string {
	return sm.GenerateWithData(nil)
}

// GetOrCreate retrieves an existing session or creates a new one if the ID is not found.
// Expired sessions are automatically cleaned up and treated as not found.
func (sm *SessionManager) GetOrCreate(sessionID string) (*Session, bool, error) {
	if sessionID == "" {
		return nil, false, errors.New("session ID cannot be empty")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check for existing session
	session, exists := sm.sessions[sessionID]
	if exists {
		// On-demand expiry check - if expired, clean up and treat as not found
		if time.Now().After(session.ExpiresAt) {
			if !session.Terminated {
				session.Terminated = true
				for _, hook := range sm.cleanupHooks {
					hook(session)
				}
			}
			delete(sm.sessions, sessionID)
			// Fall through to create new session
		} else if session.Terminated {
			return nil, false, errors.New("session is terminated")
		} else {
			// Session is valid, return a copy to prevent external mutation
			return sm.copySession(session), false, nil
		}
	}

	// Create a new session if it doesn't exist
	sm.logger.Info("Creating new session for external ID", "session_id", sessionID)
	now := time.Now()
	expiresAt := now.Add(sm.sessionDuration)
	session = &Session{
		ID:        sessionID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}
	sm.sessions[sessionID] = session

	return sm.copySession(session), true, nil
}

// Terminate marks a session as terminated and runs cleanup hooks.
func (sm *SessionManager) Terminate(sessionID string) (bool, error) {
	if sessionID == "" {
		return false, errors.New("session ID cannot be empty")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return false, errors.New(errSessionNotFound)
	}

	if !session.Terminated {
		session.Terminated = true
		for _, hook := range sm.cleanupHooks {
			hook(session)
		}
	}
	delete(sm.sessions, sessionID)
	return true, nil
}

// Get retrieves a session by its ID.
// Returns a copy of the session to prevent external mutation.
func (sm *SessionManager) Get(sessionID string) (*Session, error) {
	if sessionID == "" {
		return nil, errors.New("session ID cannot be empty")
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, errors.New(errSessionNotFound)
	}

	// Return a copy to prevent external mutation
	return sm.copySession(session), nil
}

// UpdateCredentials updates the credentials of an existing session.
// This method modifies the session in-place, unlike Get which returns a copy.
func (sm *SessionManager) UpdateCredentials(sessionID string, creds *KiteCredentials) error {
	if sessionID == "" {
		return errors.New("session ID cannot be empty")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return errors.New(errSessionNotFound)
	}

	if session.Terminated {
		return errors.New(errCannotUpdateTerminated)
	}

	// Credentials are caller-owned. Copy them so a caller cannot mutate session
	// state after releasing this lock.
	session.Credentials = copyCredentials(creds)
	// A successful reauthentication starts a fresh session lifetime. This keeps
	// the backing session available for tokens minted from the refreshed grant.
	session.ExpiresAt = time.Now().Add(sm.sessionDuration)
	return nil
}

// CleanupExpiredSessions removes expired sessions from memory.
func (sm *SessionManager) CleanupExpiredSessions() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for sessionID, session := range sm.sessions {
		if now.After(session.ExpiresAt) {
			if !session.Terminated {
				session.Terminated = true
				for _, hook := range sm.cleanupHooks {
					hook(session)
				}
			}
			delete(sm.sessions, sessionID)
			cleaned++
		}
	}

	return cleaned
}

// AddCleanupHook adds a function to be called when sessions are terminated.
func (sm *SessionManager) AddCleanupHook(hook CleanupHook) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cleanupHooks = append(sm.cleanupHooks, hook)
}

// StartCleanupRoutine starts a background goroutine to clean up expired sessions.
func (sm *SessionManager) StartCleanupRoutine(ctx context.Context) {
	go sm.cleanupRoutine(ctx)
}

// StopCleanupRoutine stops the background cleanup goroutine.
func (sm *SessionManager) StopCleanupRoutine() {
	if sm.cleanupCancel != nil {
		sm.cleanupCancel()
	}
}

func (sm *SessionManager) cleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(DefaultCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			sm.logger.Info("Session cleanup routine stopped")
			return
		case <-sm.cleanupContext.Done():
			sm.logger.Info("Session cleanup routine cancelled")
			return
		case <-ticker.C:
			cleaned := sm.CleanupExpiredSessions()
			if cleaned > 0 {
				sm.logger.Info("Cleaned up expired sessions", "count", cleaned)
			}
		}
	}
}

// Validate validates a session ID and returns its termination status.
// Expired sessions are automatically terminated and cleaned up.
func (sm *SessionManager) Validate(sessionID string) (bool, error) {
	if sessionID == "" {
		return true, errors.New("session ID cannot be empty")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return true, errors.New(errSessionNotFound)
	}

	// If expired, terminate and clean up
	if time.Now().After(session.ExpiresAt) {
		if !session.Terminated {
			session.Terminated = true
			for _, hook := range sm.cleanupHooks {
				hook(session)
			}
		}
		delete(sm.sessions, sessionID)
		return true, nil
	}

	return session.Terminated, nil
}

// copySession creates a deep copy of a session to prevent external mutation.
// This ensures that returned sessions cannot be modified by callers.
func (sm *SessionManager) copySession(original *Session) *Session {
	copy := &Session{
		ID:         original.ID,
		Terminated: original.Terminated,
		CreatedAt:  original.CreatedAt,
		ExpiresAt:  original.ExpiresAt,
		OAuthData:  original.OAuthData, // Shallow copy - assuming immutable
	}

	// Deep copy credentials if present
	copy.Credentials = copyCredentials(original.Credentials)

	return copy
}

func copyCredentials(original *KiteCredentials) *KiteCredentials {
	if original == nil {
		return nil
	}
	return &KiteCredentials{
		AccessToken: original.AccessToken,
		UserID:      original.UserID,
		ExpiresAt:   original.ExpiresAt,
	}
}
