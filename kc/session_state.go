package kc

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/zerodha/kite-mcp-server/internal/storage"
)

const sessionBucket = "sessions"

// SessionData supplies a serializable snapshot of a pending flow. The flow's
// owner holds its own lock when calling PersistSessionData.
type SessionData interface{ SessionData() (string, any) }

type storedSession struct {
	ID                   string
	CreatedAt, ExpiresAt time.Time
	Credentials          *KiteCredentials
	CredentialGeneration uint64
	DataType             string
	Data                 json.RawMessage
}

type pendingSessionData struct {
	kind string
	data json.RawMessage
}

// CredentialSnapshot retains the version used by conditional renewal without
// exposing it in ordinary session responses.
type CredentialSnapshot struct {
	Credentials *KiteCredentials
	Generation  uint64
}

func SnapshotCredentials(value *KiteCredentials) CredentialSnapshot {
	if value == nil {
		return CredentialSnapshot{}
	}
	return CredentialSnapshot{copyCredentials(value), value.generation}
}

func (s CredentialSnapshot) Restore() *KiteCredentials {
	value := copyCredentials(s.Credentials)
	if value != nil {
		value.generation = s.Generation
	}
	return value
}

func (sm *SessionManager) persistLocked(session *Session) error {
	if sm.state == nil {
		return nil
	}
	value := storedSession{ID: session.ID, CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt, Credentials: session.Credentials}
	if session.Credentials != nil {
		value.CredentialGeneration = session.Credentials.generation
	}
	if session.OAuthData != nil {
		data, ok := session.OAuthData.(SessionData)
		if !ok {
			return errors.New("pending session data cannot be persisted")
		}
		kind, snapshot := data.SessionData()
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		value.DataType, value.Data = kind, encoded
	}
	return storage.Put(sm.state, sessionBucket, session.ID, value)
}

// RestoreState must run before the manager is exposed to requests. Each flow
// owner registers its decoder during service initialization.
func (sm *SessionManager) RestoreState(state storage.State) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.state = state
	if state == nil {
		return nil
	}
	values, err := state.Load(sessionBucket)
	if err != nil {
		return err
	}
	for id, data := range values {
		var value storedSession
		if json.Unmarshal(data, &value) != nil || value.ID != id || value.ExpiresAt.IsZero() {
			return errors.New("invalid stored session")
		}
		if !time.Now().Before(value.ExpiresAt) {
			if err := storage.Delete(state, sessionBucket, id); err != nil {
				return err
			}
			continue
		}
		session := &Session{ID: id, CreatedAt: value.CreatedAt, ExpiresAt: value.ExpiresAt, Credentials: value.Credentials}
		if session.Credentials != nil {
			session.Credentials.generation = value.CredentialGeneration
		}
		if value.DataType != "" {
			session.OAuthData = pendingSessionData{value.DataType, value.Data}
			sm.pendingAuth.Set(id, value.ExpiresAt)
			if sm.nextAuthExpiry.IsZero() || value.ExpiresAt.Before(sm.nextAuthExpiry) {
				sm.nextAuthExpiry = value.ExpiresAt
			}
		}
		sm.sessions.Set(id, session)
	}
	return nil
}

func (sm *SessionManager) RegisterSessionData(kind string, decode func(string, json.RawMessage) (any, error)) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for id, session := range sm.sessions.All() {
		value, ok := session.OAuthData.(pendingSessionData)
		if !ok || value.kind != kind {
			continue
		}
		decoded, err := decode(id, value.data)
		if err != nil {
			return err
		}
		session.OAuthData = decoded
	}
	return nil
}

func (sm *SessionManager) PersistSessionData(id string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	session := sm.sessions.Get(id)
	if session == nil || session.Terminated || !time.Now().Before(session.ExpiresAt) {
		return errors.New("pending session expired")
	}
	return sm.persistLocked(session)
}

func (m *Manager) StateStore() storage.State { return m.sessionManager.state }

func (m *Manager) restoreState(state storage.State) error {
	if err := m.sessionManager.RestoreState(state); err != nil {
		return err
	}
	if state == nil {
		return nil
	}
	values, err := state.Load("session_signer")
	if err != nil {
		return err
	}
	if data, ok := values["key"]; ok {
		var key []byte
		if json.Unmarshal(data, &key) != nil || len(key) != 32 {
			return errors.New("invalid stored session signing key")
		}
		m.sessionSigner.secretKey = key
		return nil
	}
	return storage.Put(state, "session_signer", "key", m.sessionSigner.secretKey)
}

// Expiry can release thousands of admission slots. Commit the deletions once,
// before changing memory or firing hooks, rather than fsyncing each record.
func (sm *SessionManager) cleanupExpiredLocked(now time.Time, pendingOnly bool) (int, error) {
	var expired []*Session
	var pendingIDs []string
	var next time.Time
	if pendingOnly {
		for id, expiry := range sm.pendingAuth.All() {
			if !now.Before(expiry) {
				pendingIDs = append(pendingIDs, id)
				if session := sm.sessions.Get(id); session != nil && !now.Before(session.ExpiresAt) {
					expired = append(expired, session)
				}
			} else if next.IsZero() || expiry.Before(next) {
				next = expiry
			}
		}
	} else {
		for id, session := range sm.sessions.All() {
			if !now.Before(session.ExpiresAt) {
				expired = append(expired, session)
				pendingIDs = append(pendingIDs, id)
			}
		}
	}
	changes := make([]storage.Change, 0, len(expired))
	for _, session := range expired {
		changes = append(changes, storage.Change{Bucket: sessionBucket, Key: session.ID})
	}
	if sm.state != nil && len(changes) > 0 {
		if err := sm.state.Apply(changes...); err != nil {
			return 0, err
		}
	}
	for _, session := range expired {
		if !session.Terminated {
			session.Terminated = true
			for _, hook := range sm.cleanupHooks {
				hook(session)
			}
		}
		sm.sessions.Delete(session.ID)
	}
	for _, id := range pendingIDs {
		sm.pendingAuth.Delete(id)
	}
	if pendingOnly {
		sm.nextAuthExpiry = next
	}
	return len(expired), nil
}
