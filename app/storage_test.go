package app

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLiteStartupFailureReturnsErrorAndCleansUp(t *testing.T) {
	app := NewApp(slog.New(slog.NewTextHandler(io.Discard, nil)))
	app.Config = &Config{SQLitePath: filepath.Join(t.TempDir(), "missing", "state.db"), OAuthTokenTTL: "168h", JWTSecret: "01234567890123456789012345678901", OAuthIssuer: "https://mcp.example", KiteAPIKey: "key"}
	require.NotPanics(t, func() {
		server, err := app.initializeServices()
		require.Nil(t, server)
		require.ErrorContains(t, err, "parent directory")
	})
	require.Nil(t, app.state)
}

func TestSQLitePathIsLoadedFromEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	t.Setenv("SQLITE_PATH", path)
	app := NewApp(slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Equal(t, path, app.Config.SQLitePath)
}
