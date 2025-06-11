package kc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// Default session configuration
	DefaultSessionDuration = 12 * time.Hour
	DefaultCleanupInterval = 30 * time.Minute

	// Error messages
	errInvalidSessionIDFormat = "invalid session ID format"
	errSessionNotFound        = "session ID not found"
	errCannotUpdateTerminated = "cannot update terminated session"
	errCannotExtendTerminated = "cannot extend terminated session"
	mcpSessionPrefix          = "kitemcp-"
)

type MCPSession struct {
	ID         string
	Terminated bool
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Data       any // Contains KiteSessionData
}

type SessionRegistry struct {
	sessions        map[string]*MCPSession
	mu              sync.RWMutex
	sessionDuration time.Duration
	cleanupHooks    []CleanupHook
	cleanupContext  context.Context
	cleanupCancel   context.CancelFunc
	logger          *slog.Logger
}

// CleanupHook is called when a session is terminated or expires
type CleanupHook func(session *MCPSession)

// NewSessionRegistry creates a new registry that manages MCP sessions and their associated Kite data
func NewSessionRegistry(logger *slog.Logger) *SessionRegistry {
	ctx, cancel := context.WithCancel(context.Background())
	return &SessionRegistry{
		sessions:        make(map[string]*MCPSession),
		sessionDuration: DefaultSessionDuration,
		cleanupHooks:    make([]CleanupHook, 0),
		cleanupContext:  ctx,
		cleanupCancel:   cancel,
		logger:          logger,
	}
}

// NewSessionRegistryWithDuration creates a new session registry with custom duration
func NewSessionRegistryWithDuration(duration time.Duration, logger *slog.Logger) *SessionRegistry {
	ctx, cancel := context.WithCancel(context.Background())
	return &SessionRegistry{
		sessions:        make(map[string]*MCPSession),
		sessionDuration: duration,
		cleanupHooks:    make([]CleanupHook, 0),
		cleanupContext:  ctx,
		cleanupCancel:   cancel,
		logger:          logger,
	}
}

// Generate creates a new MCP session ID and stores it in memory
func (sm *SessionRegistry) Generate() string {
	return sm.GenerateWithData(nil)
}

// GenerateWithData creates a new MCP session ID with associated Kite data and stores it in memory
func (sm *SessionRegistry) GenerateWithData(data any) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sessionID := mcpSessionPrefix + uuid.New().String()
	now := time.Now()
	expiresAt := now.Add(sm.sessionDuration)

	sm.sessions[sessionID] = &MCPSession{
		ID:         sessionID,
		Terminated: false,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		Data:       data,
	}

	sm.logger.Info("Generated new MCP session ID", "session_id", sessionID, "expires_at", expiresAt)

	return sessionID
}

// should be a valid uuid and start with the correct prefix.
// checkSessionID validates the format of a MCP session ID
// Accepts both internal format (kitemcp-<uuid>) and external format (plain uuid)
func checkSessionID(sessionID string) error {
	// Handle internal format with prefix
	if strings.HasPrefix(sessionID, mcpSessionPrefix) {
		if _, err := uuid.Parse(sessionID[len(mcpSessionPrefix):]); err != nil {
			return fmt.Errorf("%s: %w", errInvalidSessionIDFormat, err)
		}
		return nil
	}

	// Handle external format (plain UUID from SSE/stdio modes)
	if _, err := uuid.Parse(sessionID); err != nil {
		return fmt.Errorf("%s: %w", errInvalidSessionIDFormat, err)
	}
	return nil
}

// Validate checks if a MCP session ID is valid and not terminated
func (sm *SessionRegistry) Validate(sessionID string) (isTerminated bool, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Log validation attempt
	sm.logger.Debug("Validating MCP session ID", "session_id", sessionID)

	if err := checkSessionID(sessionID); err != nil {
		return false, err
	}

	sm.logger.Debug("checking for session", "session_id", sessionID)
	sm.logger.Debug("sessions in map", "sessions", len(sm.sessions))
	session, exists := sm.sessions[sessionID]
	if !exists {
		sm.logger.Warn("MCP session ID not found", "session_id", sessionID)
		return false, errors.New(errSessionNotFound)
	}

	// Check if session has expired
	if time.Now().After(session.ExpiresAt) {
		sm.logger.Info("MCP session has expired", "session_id", sessionID, "expiry", session.ExpiresAt)
		session.Terminated = true
		return true, nil
	}

	// Log validation result
	// Log session status
	if session.Terminated {
		sm.logger.Debug("MCP session is already terminated", "session_id", sessionID)
	} else {
		sm.logger.Debug("MCP session is valid", "session_id", sessionID, "expires_at", session.ExpiresAt)
	}

	return session.Terminated, nil
}

// Terminate marks a MCP session ID as terminated and cleans up associated Kite session
func (sm *SessionRegistry) Terminate(sessionID string) (isNotAllowed bool, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if sessionID has the correct prefix and valid UUID format
	if err := checkSessionID(sessionID); err != nil {
		return false, err
	}

	session, exists := sm.sessions[sessionID]
	if !exists {
		return false, errors.New(errSessionNotFound)
	}

	session.Terminated = true

	// Call cleanup hooks for associated Kite sessions
	for _, hook := range sm.cleanupHooks {
		hook(session)
	}

	return false, nil
}

// GetSession retrieves a MCP session by ID (helper method)
func (sm *SessionRegistry) GetSession(sessionID string) (*MCPSession, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, errors.New(errSessionNotFound)
	}

	return session, nil
}

// ListActiveSessions returns all non-terminated MCP sessions (helper method)
func (sm *SessionRegistry) ListActiveSessions() []*MCPSession {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var activeSessions []*MCPSession
	now := time.Now()

	for _, session := range sm.sessions {
		// Auto-expire sessions that have passed their expiration time
		if now.After(session.ExpiresAt) {
			session.Terminated = true
		}

		if !session.Terminated {
			activeSessions = append(activeSessions, session)
		}
	}

	return activeSessions
}

// Note: ExtendSession method has been removed to enforce fixed session durations

// CleanupExpiredSessions removes expired MCP sessions from memory and their associated Kite data
func (sm *SessionRegistry) CleanupExpiredSessions() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for sessionID, session := range sm.sessions {
		if now.After(session.ExpiresAt) {
			// Mark as terminated and call cleanup hooks
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

// GetSessionDuration returns the configured MCP session duration
func (sm *SessionRegistry) GetSessionDuration() time.Duration {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessionDuration
}

// SetSessionDuration updates the MCP session duration for new sessions
func (sm *SessionRegistry) SetSessionDuration(duration time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessionDuration = duration
}

// AddCleanupHook adds a cleanup function for the Kite session to be called when MCP sessions are terminated
func (sm *SessionRegistry) AddCleanupHook(hook CleanupHook) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cleanupHooks = append(sm.cleanupHooks, hook)
}

// StartCleanupRoutine starts background cleanup goroutines for expired MCP sessions
func (sm *SessionRegistry) StartCleanupRoutine(ctx context.Context) {
	go sm.cleanupRoutine(ctx)
}

// StopCleanupRoutine stops background cleanup goroutines for MCP sessions
func (sm *SessionRegistry) StopCleanupRoutine() {
	if sm.cleanupCancel != nil {
		sm.cleanupCancel()
	}
}

// cleanupRoutine runs periodic cleanup of expired MCP sessions and their Kite data
func (sm *SessionRegistry) cleanupRoutine(ctx context.Context) {
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

// UpdateSessionData updates the Kite data for an existing MCP session
func (sm *SessionRegistry) UpdateSessionData(sessionID string, data any) error {
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

// GetSessionData retrieves the Kite data for a MCP session
func (sm *SessionRegistry) GetSessionData(sessionID string) (any, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sm.logger.Debug("Getting data for session ID", "session_id", sessionID)

	session, exists := sm.sessions[sessionID]
	if !exists {
		sm.logger.Warn("Session data not found for ID", "session_id", sessionID)
		return nil, errors.New(errSessionNotFound)
	}

	// Check if session has expired
	if time.Now().After(session.ExpiresAt) {
		sm.logger.Info("Session has expired, cannot get data", "session_id", sessionID)
		return nil, errors.New(errSessionNotFound)
	}

	if session.Terminated {
		sm.logger.Info("Session is terminated, cannot get data", "session_id", sessionID)
		return nil, errors.New(errSessionNotFound)
	}

	sm.logger.Debug("Successfully retrieved data for session ID", "session_id", sessionID)
	return session.Data, nil
}

// GetOrCreateSessionData atomically validates session and retrieves/creates data to eliminate TOCTOU races
func (sm *SessionRegistry) GetOrCreateSessionData(sessionID string, createDataFn func() any) (data any, isNew bool, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.logger.Debug("Getting or creating data for session ID", "session_id", sessionID)

	// Check session ID format
	if err := checkSessionID(sessionID); err != nil {
		return nil, false, err
	}

	session, exists := sm.sessions[sessionID]
	if !exists {
		// Create a new session for external session IDs (from SSE/stdio modes)
		sm.logger.Info("Creating new session for external session ID", "session_id", sessionID)
		now := time.Now()
		expiresAt := now.Add(sm.sessionDuration)

		session = &MCPSession{
			ID:         sessionID,
			Terminated: false,
			CreatedAt:  now,
			ExpiresAt:  expiresAt,
			Data:       nil,
		}
		sm.sessions[sessionID] = session
	}

	now := time.Now()

	// Check if session has expired
	if now.After(session.ExpiresAt) {
		sm.logger.Info("Session has expired", "session_id", sessionID, "expiry", session.ExpiresAt)
		session.Terminated = true
		return nil, false, errors.New(errSessionNotFound)
	}

	if session.Terminated {
		sm.logger.Info("Session is terminated, cannot get/create data", "session_id", sessionID)
		return nil, false, errors.New(errSessionNotFound)
	}

	// If data exists and is valid, return it
	if session.Data != nil {
		sm.logger.Debug("Successfully retrieved existing data for session ID", "session_id", sessionID)
		return session.Data, false, nil
	}

	// Create new data using the provided function
	sm.logger.Debug("Creating new data for session ID", "session_id", sessionID)
	newData := createDataFn()
	session.Data = newData

	sm.logger.Debug("Successfully created new data for session ID", "session_id", sessionID)
	return newData, true, nil
}
