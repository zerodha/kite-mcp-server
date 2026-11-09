package storage

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func openTestSQLite(t *testing.T, path string) *SQLite {
	t.Helper()
	s, err := OpenSQLite(path, bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSQLiteReopenAndEncryption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s := openTestSQLite(t, path)
	require.NoError(t, Put(s, "sessions", "private-session-id", map[string]string{"access_token": "private-access-token"}))
	var mode string
	var syncMode int
	require.NoError(t, s.db.QueryRow("PRAGMA journal_mode").Scan(&mode))
	require.Equal(t, "wal", mode)
	require.NoError(t, s.db.QueryRow("PRAGMA synchronous").Scan(&syncMode))
	require.Equal(t, 2, syncMode)
	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		require.NotContains(t, string(data), "private-access-token")
		require.NotContains(t, string(data), "private-session-id")
		if runtime.GOOS != "windows" {
			info, err := os.Stat(file)
			require.NoError(t, err)
			require.Zero(t, info.Mode().Perm()&0077)
		}
	}
	require.NoError(t, s.Close())
	s = openTestSQLite(t, path)
	values, err := s.Load("sessions")
	require.NoError(t, err)
	require.JSONEq(t, `{"access_token":"private-access-token"}`, string(values["private-session-id"]))
	require.NoError(t, s.Close())
	_, err = OpenSQLite(path, bytes.Repeat([]byte{8}, 32))
	require.ErrorContains(t, err, "decryption")
	// A failed wrong-key startup must not alter the original state.
	s = openTestSQLite(t, path)
	values, err = s.Load("sessions")
	require.NoError(t, err)
	require.Len(t, values, 1)
}

func TestSQLiteRejectsSecondOwnerAndAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	s := openTestSQLite(t, path)
	_, err := OpenSQLite(path, bytes.Repeat([]byte{7}, 32))
	require.ErrorContains(t, err, "owned")
	alias := filepath.Join(dir, "alias.db")
	if err := os.Symlink(path, alias); err == nil {
		_, err = OpenSQLite(alias, bytes.Repeat([]byte{7}, 32))
		require.ErrorContains(t, err, "owned")
	}
	hardlink := filepath.Join(dir, "hardlink.db")
	if err := os.Link(path, hardlink); err == nil {
		_, err = OpenSQLite(hardlink, bytes.Repeat([]byte{7}, 32))
		require.ErrorContains(t, err, "hard links")
		require.NoError(t, os.Remove(hardlink))
	}
	require.NoError(t, s.Close())
	openTestSQLite(t, path)
}

func TestSQLiteRollbackAndFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s := openTestSQLite(t, path)
	require.NoError(t, Put(s, "permissions", "session", false))
	change, err := Encode("permissions", "session", true)
	require.NoError(t, err)
	require.Error(t, s.Apply(change, Change{Bucket: "approvals", Key: "bad", Value: json.RawMessage(`invalid`)}))
	require.ErrorIs(t, s.Check(), ErrUnavailable)
	require.ErrorIs(t, Put(s, "permissions", "other", true), ErrUnavailable)
	require.NoError(t, s.Close())
	s = openTestSQLite(t, path)
	values, err := s.Load("permissions")
	require.NoError(t, err)
	require.Equal(t, "false", string(values["session"]))
	require.Len(t, values, 1)
}

func TestSQLiteRequiresExistingDirectory(t *testing.T) {
	_, err := OpenSQLite(filepath.Join(t.TempDir(), "missing", "state.db"), bytes.Repeat([]byte{7}, 32))
	require.ErrorContains(t, err, "parent directory")
}

func TestSQLiteCommitSurvivesProcessExit(t *testing.T) {
	const env = "KITE_SQLITE_CRASH_TEST_PATH"
	if path := os.Getenv(env); path != "" {
		s, err := OpenSQLite(path, bytes.Repeat([]byte{7}, 32))
		if err != nil {
			os.Exit(2)
		}
		// No Close: leave the committed record in WAL and let the OS release locks.
		if Put(s, "approvals", "once", "submitting") != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "state.db")
	cmd := exec.Command(os.Args[0], "-test.run=^TestSQLiteCommitSurvivesProcessExit$")
	cmd.Env = append(os.Environ(), env+"="+path)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	s := openTestSQLite(t, path)
	values, err := s.Load("approvals")
	require.NoError(t, err)
	require.Equal(t, `"submitting"`, string(values["once"]))
}

func TestSQLiteRejectsOtherApplicationsDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.db")
	db, err := sql.Open("sqlite", sqliteURI(path))
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE unrelated(value TEXT)")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, os.Chmod(path, 0640))
	_, err = OpenSQLite(path, bytes.Repeat([]byte{7}, 32))
	require.ErrorContains(t, err, "another database")
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0640), info.Mode().Perm())
	}
	db, err = sql.Open("sqlite", sqliteURI(path))
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	var mode string
	require.NoError(t, db.QueryRow("PRAGMA journal_mode").Scan(&mode))
	require.Equal(t, "delete", mode)
}
