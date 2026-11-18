package kc

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zerodha/kite-mcp-server/internal/storage"
)

const (
	// Default session configuration
	// Keep the backing session alive for at least the default OAuth token
	// lifetime. Callers that configure longer-lived tokens should pass that
	// duration through NewSessionManagerWithDuration.
	DefaultSessionDuration         = 7 * 24 * time.Hour
	DefaultPendingSessionDuration  = 10 * time.Minute
	DefaultCleanupInterval         = 30 * time.Minute
	maxPendingAuthorizations       = 10000
	maxPendingReviewAuthorizations = 1024
	maxPendingReviewsPerOwner      = 8
	maxPendingReviewsPerAccount    = 32

	// Error messages
	errSessionNotFound        = "session ID not found"
	errCannotUpdateTerminated = "cannot update terminated session"
	mcpSessionPrefix          = "kitemcp-"
)

// ErrPendingAuthorizationLimit means all pending OAuth flow slots are occupied.
var ErrPendingAuthorizationLimit = errors.New("too many pending authorizations")

// ReviewAuthorization identifies a short-lived proof for an existing grant.
// Browser proofs are admitted only after matching-account sign-in succeeds.
// Their admission pool is separate from unauthenticated OAuth consent flows.
type ReviewAuthorization interface {
	ReviewAuthorization() (owner string, expires time.Time)
}

// Session represents a single, long-lived user/client session.
type Session struct {
	ID             string
	Terminated     bool
	CreatedAt      time.Time
	ExpiresAt      time.Time
	Credentials    *KiteCredentials // Holds the short-lived Kite credentials.
	OAuthData      any              // Holds temporary data for an in-progress OAuth flow.
	refreshRetryAt time.Time
	refreshFailure error
}

// SessionManager manages all active sessions.
type SessionManager struct {
	state                   storage.State
	sessions                storage.Records[string, *Session]
	pendingAuth             storage.Records[string, time.Time]
	pendingReviews          storage.Records[string, time.Time]
	statelessAuthorizations map[string]func(string) (any, time.Time, error)
	nextAuthExpiry          time.Time
	mu                      sync.RWMutex
	sessionDuration         time.Duration
	cleanupHooks            []CleanupHook
	cleanupContext          context.Context
	cleanupCancel           context.CancelFunc
	logger                  *slog.Logger
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
		sessions:                storage.NewMemory[string, *Session](),
		pendingAuth:             storage.NewMemory[string, time.Time](),
		pendingReviews:          storage.NewMemory[string, time.Time](),
		statelessAuthorizations: make(map[string]func(string) (any, time.Time, error)),
		sessionDuration:         sessionDuration,
		cleanupHooks:            make([]CleanupHook, 0),
		cleanupContext:          ctx,
		cleanupCancel:           cancel,
		logger:                  logger,
	}
}

// GenerateWithData creates a unique session. Pending authorization data uses
// a short lifetime independently of authenticated application grants.
func (sm *SessionManager) GenerateWithData(initialData any) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	id, err := sm.generateWithDataLocked(initialData)
	if err != nil {
		sm.logger.Error("Session creation failed", "error", err)
	}
	return id
}

// GeneratePendingAuthorization admits an unauthenticated OAuth flow without
// letting abandoned requests consume unbounded memory or evict live grants.
func (sm *SessionManager) GeneratePendingAuthorization(initialData any) (string, error) {
	if initialData == nil {
		return "", errors.New("pending authorization data is required")
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	now := time.Now()
	if review, ok := initialData.(ReviewAuthorization); ok {
		owner, expires := review.ReviewAuthorization()
		grant := sm.sessions.Get(owner)
		if grant == nil || grant.Terminated || grant.Credentials == nil || !now.Before(grant.ExpiresAt) || !now.Before(expires) {
			return "", errors.New("review grant unavailable")
		}
		// Reclaim abandoned reviews at admission, including those whose parent
		// grant ended. OAuth admission never counts these review slots.
		if _, err := sm.cleanupExpiredLocked(now, true); err != nil {
			return "", err
		}
		owned, accountOwned := 0, 0
		for id := range sm.pendingReviews.All() {
			if pending := sm.sessions.Get(id); pending != nil {
				if data, ok := pending.OAuthData.(ReviewAuthorization); ok {
					parent, _ := data.ReviewAuthorization()
					if parent == owner {
						owned++
					}
					if parentGrant := sm.sessions.Get(parent); parentGrant != nil && parentGrant.Credentials != nil && parentGrant.Credentials.UserID == grant.Credentials.UserID {
						accountOwned++
					}
				}
			}
		}
		if sm.pendingReviews.Len() >= maxPendingReviewAuthorizations || owned >= maxPendingReviewsPerOwner || accountOwned >= maxPendingReviewsPerAccount {
			return "", ErrPendingAuthorizationLimit
		}
		id, err := sm.generateWithDataLocked(initialData)
		if err == nil {
			sm.pendingReviews.Set(id, sm.sessions.Get(id).ExpiresAt)
		}
		return id, err
	}
	if sm.pendingAuth.Len() >= maxPendingAuthorizations && !now.Before(sm.nextAuthExpiry) {
		if _, err := sm.cleanupExpiredLocked(now, true); err != nil {
			return "", err
		}
	}
	if sm.pendingAuth.Len() >= maxPendingAuthorizations {
		return "", ErrPendingAuthorizationLimit
	}
	id, err := sm.generateWithDataLocked(initialData)
	if err != nil {
		return "", err
	}
	expiry := sm.sessions.Get(id).ExpiresAt
	sm.pendingAuth.Set(id, expiry)
	if sm.nextAuthExpiry.IsZero() || expiry.Before(sm.nextAuthExpiry) {
		sm.nextAuthExpiry = expiry
	}
	return id, nil
}

func (sm *SessionManager) generateWithDataLocked(initialData any) (string, error) {
	sessionID := mcpSessionPrefix + uuid.New().String()
	now := time.Now()
	expiresAt := now.Add(sm.sessionDuration)
	if initialData != nil {
		expiresAt = now.Add(DefaultPendingSessionDuration)
	}
	if review, ok := initialData.(ReviewAuthorization); ok {
		owner, deadline := review.ReviewAuthorization()
		if deadline.Before(expiresAt) {
			expiresAt = deadline
		}
		if grant := sm.sessions.Get(owner); grant != nil && grant.ExpiresAt.Before(expiresAt) {
			expiresAt = grant.ExpiresAt
		}
	}

	session := &Session{
		ID:        sessionID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		OAuthData: initialData,
	}
	if err := sm.persistLocked(session); err != nil {
		return "", err
	}
	sm.sessions.Set(sessionID, session)

	sm.logger.Info("Generated new session", "session_id", sessionID, "expires_at", expiresAt)
	return sessionID, nil
}

// Generate creates a new session and satisfies the server.SessionIdManager interface.
func (sm *SessionManager) Generate() string {
	return sm.GenerateWithData(nil)
}

// GetOrCreate retrieves an existing session or creates a new one if the ID is not found.
// Expired sessions are automatically cleaned up and treated as not found.
func (sm *SessionManager) GetOrCreate(sessionID string) (*Session, bool, error) {
	if err := storage.Check(sm.state); err != nil {
		return nil, false, err
	}
	if sessionID == "" {
		return nil, false, errors.New("session ID cannot be empty")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check for existing session
	session, exists := sm.sessions.Lookup(sessionID)
	if exists {
		// On-demand expiry check - if expired, clean up and treat as not found
		if time.Now().After(session.ExpiresAt) {
			if !session.Terminated {
				session.Terminated = true
				for _, hook := range sm.cleanupHooks {
					hook(session)
				}
			}
			if err := storage.Delete(sm.state, sessionBucket, sessionID); err != nil {
				return nil, false, err
			}
			sm.sessions.Delete(sessionID)
			sm.pendingAuth.Delete(sessionID)
			sm.pendingReviews.Delete(sessionID)
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
	if err := sm.persistLocked(session); err != nil {
		return nil, false, err
	}
	sm.sessions.Set(sessionID, session)

	return sm.copySession(session), true, nil
}

// Terminate marks a session as terminated and runs cleanup hooks.
func (sm *SessionManager) Terminate(sessionID string) (bool, error) {
	if err := storage.Check(sm.state); err != nil {
		return false, err
	}
	if sessionID == "" {
		return false, errors.New("session ID cannot be empty")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions.Lookup(sessionID)
	if !exists {
		return false, errors.New(errSessionNotFound)
	}

	if !session.Terminated {
		session.Terminated = true
		for _, hook := range sm.cleanupHooks {
			hook(session)
		}
	}
	if err := storage.Delete(sm.state, sessionBucket, sessionID); err != nil {
		return false, err
	}
	sm.sessions.Delete(sessionID)
	sm.pendingAuth.Delete(sessionID)
	sm.pendingReviews.Delete(sessionID)
	return true, nil
}

// Get retrieves a session by its ID.
// Returns a copy of the session to prevent external mutation.
func (sm *SessionManager) Get(sessionID string) (*Session, error) {
	if err := storage.Check(sm.state); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, errors.New("session ID cannot be empty")
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions.Lookup(sessionID)
	if !exists {
		for prefix, decode := range sm.statelessAuthorizations {
			if strings.HasPrefix(sessionID, prefix) {
				data, expires, err := decode(sessionID)
				if err != nil {
					return nil, err
				}
				if !time.Now().Before(expires) {
					return nil, errors.New("authorization expired")
				}
				return &Session{ID: sessionID, ExpiresAt: expires, OAuthData: data}, nil
			}
		}
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

	return sm.updateCredentialsLocked(sessionID, creds)
}

// ReplaceApprovalCredentials installs fresh credentials only if the original
// account credentials remain current. It never revives or extends a grant.
func (sm *SessionManager) ReplaceApprovalCredentials(sessionID string, previous, fresh *KiteCredentials) error {
	if previous == nil || fresh == nil || previous.UserID != fresh.UserID {
		return errors.New("approval account mismatch")
	}
	return sm.updateRenewedCredentials(sessionID, previous.generation, fresh)
}

// updateRenewedCredentials cannot replace credentials installed while renewal
// was in flight. The original refresh token belongs to one credential version.
func (sm *SessionManager) updateRenewedCredentials(sessionID string, generation uint64, creds *KiteCredentials) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	session := sm.sessions.Get(sessionID)
	if session == nil || session.Credentials == nil || session.Credentials.generation != generation {
		return errors.New("session credentials changed during token renewal")
	}
	return sm.updateCredentialsLocked(sessionID, creds)
}

func (sm *SessionManager) updateCredentialsLocked(sessionID string, creds *KiteCredentials) error {
	if err := storage.Check(sm.state); err != nil {
		return err
	}
	session, exists := sm.sessions.Lookup(sessionID)
	if !exists {
		return errors.New(errSessionNotFound)
	}

	if session.Terminated {
		return errors.New(errCannotUpdateTerminated)
	}
	if !time.Now().Before(session.ExpiresAt) {
		return errors.New("session has expired")
	}

	// Credentials are caller-owned. Copy them so a caller cannot mutate session
	// state after releasing this lock.
	updated := copyCredentials(creds)
	if updated != nil {
		updated.generation = 1
		if session.Credentials != nil {
			updated.generation = session.Credentials.generation + 1
		}
	}
	session.Credentials = updated
	session.refreshRetryAt = time.Time{}
	session.refreshFailure = nil
	// Credential renewal preserves the fixed grant deadline. A new login
	// creates a fresh opaque grant instead of extending an existing one.
	return sm.persistLocked(session)
}

// A rejection of an old refresh token must not log out replacement credentials.
func (sm *SessionManager) terminateRejectedRefresh(sessionID string, generation uint64) {
	if err := storage.Check(sm.state); err != nil {
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	session := sm.sessions.Get(sessionID)
	if session == nil || session.Credentials == nil || session.Credentials.generation != generation {
		return
	}
	if !session.Terminated {
		session.Terminated = true
		for _, hook := range sm.cleanupHooks {
			hook(session)
		}
	}
	if err := storage.Delete(sm.state, sessionBucket, sessionID); err != nil {
		return
	}
	sm.sessions.Delete(sessionID)
	sm.pendingAuth.Delete(sessionID)
	sm.pendingReviews.Delete(sessionID)
}

// rejectAccessToken invalidates only the credential version that was rejected.
// A delayed response from an old request must not invalidate a newer token.
func (sm *SessionManager) rejectAccessToken(sessionID, accessToken string, generation uint64) {
	if err := storage.Check(sm.state); err != nil {
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	session := sm.sessions.Get(sessionID)
	if session != nil && session.Credentials != nil && session.Credentials.AccessToken == accessToken && session.Credentials.generation == generation {
		session.Credentials.ExpiresAt = time.Time{}
		if err := sm.persistLocked(session); err != nil {
			sm.logger.Error("Credential invalidation persistence failed", "error", err)
		}
	}
}

func (sm *SessionManager) recordRefreshFailure(sessionID string, generation uint64, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if session := sm.sessions.Get(sessionID); session != nil && session.Credentials != nil && session.Credentials.generation == generation {
		session.refreshFailure = err
		session.refreshRetryAt = time.Now().Add(5 * time.Second)
	}
}

// CleanupExpiredSessions removes expired sessions from memory.
func (sm *SessionManager) CleanupExpiredSessions() int {
	if err := storage.Check(sm.state); err != nil {
		return 0
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cleaned, err := sm.cleanupExpiredLocked(time.Now(), false)
	if err != nil {
		sm.logger.Error("Session cleanup persistence failed", "error", err)
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
	if err := storage.Check(sm.state); err != nil {
		return true, err
	}
	if sessionID == "" {
		return true, errors.New("session ID cannot be empty")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions.Lookup(sessionID)
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
		if err := storage.Delete(sm.state, sessionBucket, sessionID); err != nil {
			return true, err
		}
		sm.sessions.Delete(sessionID)
		sm.pendingAuth.Delete(sessionID)
		sm.pendingReviews.Delete(sessionID)
		return true, nil
	}

	return session.Terminated, nil
}

// copySession creates a deep copy of a session to prevent external mutation.
// This ensures that returned sessions cannot be modified by callers.
func (sm *SessionManager) copySession(original *Session) *Session {
	copy := &Session{
		ID:             original.ID,
		Terminated:     original.Terminated,
		CreatedAt:      original.CreatedAt,
		ExpiresAt:      original.ExpiresAt,
		OAuthData:      original.OAuthData, // Shallow copy - assuming immutable
		refreshRetryAt: original.refreshRetryAt,
		refreshFailure: original.refreshFailure,
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
		AccessToken:  original.AccessToken,
		RefreshToken: original.RefreshToken,
		UserID:       original.UserID,
		ExpiresAt:    original.ExpiresAt,
		generation:   original.generation,
	}
}

// RegisterStatelessAuthorization routes sealed callback state without admitting
// a pending session. Decoders run under a read lock and must not call managers.
func (sm *SessionManager) RegisterStatelessAuthorization(prefix string, decode func(string) (any, time.Time, error)) error {
	if prefix == "" || decode == nil {
		return errors.New("invalid stateless authorization decoder")
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.statelessAuthorizations[prefix] = decode
	return nil
}
