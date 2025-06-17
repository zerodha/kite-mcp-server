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
	DefaultSessionDuration = 12 * time.Hour
	DefaultCleanupInterval = 30 * time.Minute

	// Error messages
	errSessionNotFound        = "session ID not found"
	errCannotUpdateTerminated = "cannot update terminated session"
	mcpSessionPrefix          = "kitemcp-"
)

// Session represents a single user/client session.
type Session struct {
	ID         string
	Terminated bool
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Data       any // Can hold KiteSessionData or temporary OAuth data.
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
	ctx, cancel := context.WithCancel(context.Background())
	return &SessionManager{
		sessions:        make(map[string]*Session),
		sessionDuration: DefaultSessionDuration,
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
		ID:         sessionID,
		Terminated: false,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		Data:       initialData,
	}

	sm.logger.Info("Generated new session", "session_id", sessionID, "expires_at", expiresAt)
	return sessionID
}

// Generate creates a new session and satisfies the server.SessionIdManager interface.
func (sm *SessionManager) Generate() string {
	return sm.GenerateWithData(nil)
}

// GetOrCreate retrieves an existing session or creates a new one if the ID is not found.
// This is used for sessions where the ID is provided by an external source (OAuth UserID, SSE client).
func (sm *SessionManager) GetOrCreate(sessionID string) (*Session, bool, error) {
	if sessionID == "" {
		return nil, false, errors.New("session ID cannot be empty")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check for existing session
	session, exists := sm.sessions[sessionID]
	if exists {
		// On-demand expiry check
		if time.Now().After(session.ExpiresAt) {
			session.Terminated = true
		}
		// Ensure it's not terminated
		if session.Terminated {
			return nil, false, errors.New("session is terminated")
		}
		return session, false, nil
	}

	// Create a new session if it doesn't exist
	sm.logger.Info("Creating new session for external ID", "session_id", sessionID)
	now := time.Now()
	expiresAt := now.Add(sm.sessionDuration)
	session = &Session{
		ID:         sessionID,
		Terminated: false,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		Data:       nil, // Data is populated by the caller
	}
	sm.sessions[sessionID] = session

	return session, true, nil
}

// Terminate marks a session as terminated and runs cleanup hooks.
// It returns (bool, error) to satisfy the server.SessionIdManager interface.
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

	// Also delete it from the map
	delete(sm.sessions, sessionID)

	return true, nil
}

// Get retrieves a session by its ID.
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

	return session, nil
}

// UpdateSessionData updates the data for an existing session.
func (sm *SessionManager) UpdateSessionData(sessionID string, data any) error {
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

	session.Data = data
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

// cleanupRoutine runs periodic cleanup of expired sessions.
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

// GetSessionCount returns the number of active sessions.
func (sm *SessionManager) GetSessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// Validate validates a session ID and returns its termination status.
func (sm *SessionManager) Validate(sessionID string) (bool, error) {
	if sessionID == "" {
		return true, errors.New("session ID cannot be empty") // Is terminated, with error
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return true, errors.New(errSessionNotFound) // Is terminated, with error
	}

	if time.Now().After(session.ExpiresAt) {
		return true, nil // Expired is a form of terminated.
	}

	return session.Terminated, nil
}
