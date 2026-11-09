package kc

import (
	"bytes"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/internal/storage"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

func persistentSessionManager(t *testing.T, path string) (*Manager, *storage.SQLite) {
	t.Helper()
	state, err := storage.OpenSQLite(path, bytes.Repeat([]byte{9}, 32))
	require.NoError(t, err)
	m, err := New(Config{APIKey: "key", APISecret: "secret", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Instruments: &instruments.Manager{}, State: state})
	require.NoError(t, err)
	t.Cleanup(func() { m.SessionManager().StopCleanupRoutine(); _ = state.Close() })
	return m, state
}

func TestSessionsSurviveRestartWithFixedExpiryAndCredentialVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	m, state := persistentSessionManager(t, path)
	id := m.SessionManager().Generate()
	require.NotEmpty(t, id)
	creds := &KiteCredentials{UserID: "user", AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, m.SessionManager().UpdateCredentials(id, creds))
	require.NoError(t, m.SessionManager().UpdateCredentials(id, creds))
	old, err := m.SessionManager().Get(id)
	require.NoError(t, err)
	signed := m.SessionSigner().SignSessionID(id)
	m.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	m, state = persistentSessionManager(t, path)
	got, err := m.SessionManager().Get(id)
	require.NoError(t, err)
	require.Equal(t, old.ExpiresAt.UnixNano(), got.ExpiresAt.UnixNano())
	require.Equal(t, old.Credentials.AccessToken, got.Credentials.AccessToken)
	require.Equal(t, old.Credentials.RefreshToken, got.Credentials.RefreshToken)
	require.Equal(t, old.Credentials.UserID, got.Credentials.UserID)
	require.Equal(t, old.Credentials.generation, got.Credentials.generation)
	require.True(t, old.Credentials.ExpiresAt.Equal(got.Credentials.ExpiresAt))
	verified, err := m.SessionSigner().VerifySessionID(signed)
	require.NoError(t, err)
	require.Equal(t, id, verified)
	require.NoError(t, m.SessionManager().ReplaceApprovalCredentials(id, got.Credentials, creds))
	require.Error(t, m.SessionManager().ReplaceApprovalCredentials(id, got.Credentials, creds), "old version must remain stale after renewal")
	_, err = m.SessionManager().Terminate(id)
	require.NoError(t, err)
	m.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	m, _ = persistentSessionManager(t, path)
	_, err = m.SessionManager().Get(id)
	require.Error(t, err, "logout must survive restart")
}

func TestExpiredAndRejectedCredentialsCannotReviveAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	m, state := persistentSessionManager(t, path)
	id := m.SessionManager().Generate()
	creds := &KiteCredentials{UserID: "user", AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, m.SessionManager().UpdateCredentials(id, creds))
	current, err := m.SessionManager().Get(id)
	require.NoError(t, err)
	m.SessionManager().rejectAccessToken(id, "access", current.Credentials.generation)
	m.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	m, state = persistentSessionManager(t, path)
	current, err = m.SessionManager().Get(id)
	require.NoError(t, err)
	require.True(t, current.Credentials.ExpiresAt.IsZero())
	m.SessionManager().terminateRejectedRefresh(id, current.Credentials.generation)
	m.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	m, state = persistentSessionManager(t, path)
	_, err = m.SessionManager().Get(id)
	require.Error(t, err)
	require.NoError(t, state.Close())
	_, _, err = m.SessionManager().GetOrCreate("another")
	require.Error(t, err)
}

func TestExpiredSessionsAreRemovedOnRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	m, state := persistentSessionManager(t, path)
	require.NoError(t, storage.Put(state, sessionBucket, "expired", storedSession{ID: "expired", CreatedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour), Credentials: &KiteCredentials{UserID: "user", AccessToken: "access"}, CredentialGeneration: 1}))
	m.SessionManager().StopCleanupRoutine()
	require.NoError(t, state.Close())
	m, state = persistentSessionManager(t, path)
	_, err := m.SessionManager().Get("expired")
	require.Error(t, err)
	values, err := state.Load(sessionBucket)
	require.NoError(t, err)
	require.Empty(t, values)
}
