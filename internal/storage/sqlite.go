package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
)

var ErrUnavailable = errors.New("persistent state unavailable")

// SQLite owns one database for one application process. Values and identifiers
// are encrypted/authenticated with a key derived from the instance's secrets.
// A write failure latches the store closed to authorization until restart.
type SQLite struct {
	db   *sql.DB
	lock *flock.Flock
	aead cipher.AEAD
	key  []byte
	// Protect the full lifetime of transactions and result rows independently
	// of the failure latch, which Close sets before waiting for operations.
	operations sync.RWMutex
	mu         sync.RWMutex
	failed     error
}

func sqliteURI(path string) string {
	path = filepath.ToSlash(path)
	// A drive-qualified Windows path needs a leading slash in a file URI.
	// Without it, net/url serializes the drive as an authority (file://C:/...).
	if len(path) >= 3 && path[1] == ':' && path[2] == '/' {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func OpenSQLite(path string, key []byte) (_ *SQLite, err error) {
	if len(key) != 32 {
		return nil, errors.New("SQLite encryption key must be 32 bytes")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Do not create directories on a potentially missing data volume. Deployment
	// configuration must separately ensure the expected volume is mounted.
	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		return nil, errors.New("SQLite parent directory must exist on the persistent volume")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	path = filepath.Join(parent, filepath.Base(path))
	if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
		path = resolved
	} else if !errors.Is(resolveErr, os.ErrNotExist) {
		return nil, resolveErr
	} else if _, linkErr := os.Lstat(path); linkErr == nil {
		return nil, errors.New("SQLite path must not be a dangling symlink")
	} else if !errors.Is(linkErr, os.ErrNotExist) {
		return nil, linkErr
	}
	lock := flock.New(path + ".lock")
	locked, err := lock.TryLock()
	if err != nil || !locked {
		_ = lock.Close()
		return nil, errors.New("SQLite database is already owned by another process or cannot be locked")
	}
	defer func() {
		if err != nil {
			_ = lock.Close()
		}
	}()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = validateDatabaseFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	uri := sqliteURI(path)
	newDatabase, inspectErr := inspectSQLite(uri)
	if inspectErr != nil {
		_ = file.Close()
		return nil, inspectErr
	}
	if err = file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", uri+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()
	db.SetMaxOpenConns(1)
	if newDatabase {
		tx, beginErr := db.Begin()
		if beginErr != nil {
			return nil, beginErr
		}
		defer func() { _ = tx.Rollback() }() // Already committed on success.
		if _, err = tx.Exec("CREATE TABLE state (bucket TEXT NOT NULL, id TEXT NOT NULL, value BLOB NOT NULL, PRIMARY KEY(bucket, id))"); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(fmt.Sprintf("PRAGMA application_id=%d", sqliteApplicationID)); err != nil {
			return nil, err
		}
		if _, err = tx.Exec("PRAGMA user_version=1"); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	s := &SQLite{db: db, lock: lock, aead: aead, key: append([]byte(nil), key...)}
	identity, err := s.Load("system")
	if err != nil {
		return nil, err
	}
	if len(identity) == 0 {
		if err = Put(s, "system", "identity", "kite-mcp-state-v1"); err != nil {
			return nil, err
		}
	} else if string(identity["identity"]) != `"kite-mcp-state-v1"` {
		return nil, errors.New("invalid SQLite state identity")
	}
	return s, nil
}

func (s *SQLite) Check() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.failed
}

func (s *SQLite) fail(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil {
		s.failed = fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return s.failed
}

func (s *SQLite) identifier(bucket, key string) string {
	m := hmac.New(sha256.New, s.key)
	_, _ = m.Write([]byte(bucket + "\x00" + key))
	return hex.EncodeToString(m.Sum(nil))
}

type encryptedRecord struct {
	Key   string
	Value json.RawMessage
}

func (s *SQLite) Load(bucket string) (map[string]json.RawMessage, error) {
	s.operations.RLock()
	defer s.operations.RUnlock()
	if err := s.Check(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query("SELECT id, value FROM state WHERE bucket = ?", bucket)
	if err != nil {
		return nil, s.fail(err)
	}
	defer func() { _ = rows.Close() }() // Iteration errors are checked below.
	values := make(map[string]json.RawMessage)
	for rows.Next() {
		var id string
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			return nil, s.fail(err)
		}
		if len(data) < s.aead.NonceSize() {
			return nil, s.fail(errors.New("invalid encrypted state"))
		}
		plain, err := s.aead.Open(nil, data[:s.aead.NonceSize()], data[s.aead.NonceSize():], []byte(bucket+"\x00"+id))
		if err != nil {
			return nil, s.fail(errors.New("state decryption failed; verify the instance secret and issuer"))
		}
		var value encryptedRecord
		if json.Unmarshal(plain, &value) != nil || s.identifier(bucket, value.Key) != id || !json.Valid(value.Value) {
			return nil, s.fail(errors.New("invalid persistent state record"))
		}
		values[value.Key] = value.Value
	}
	if err := rows.Err(); err != nil {
		return nil, s.fail(err)
	}
	return values, nil
}

func (s *SQLite) Apply(changes ...Change) error {
	s.operations.RLock()
	defer s.operations.RUnlock()
	if err := s.Check(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return s.fail(err)
	}
	defer func() { _ = tx.Rollback() }() // Already committed on success.
	if err := s.Check(); err != nil {
		return err
	}
	for _, change := range changes {
		id := s.identifier(change.Bucket, change.Key)
		if change.Value == nil {
			_, err = tx.Exec("DELETE FROM state WHERE bucket = ? AND id = ?", change.Bucket, id)
		} else {
			if !json.Valid(change.Value) {
				return s.fail(errors.New("invalid state JSON"))
			}
			plain, marshalErr := json.Marshal(encryptedRecord{change.Key, change.Value})
			if marshalErr != nil {
				return s.fail(marshalErr)
			}
			nonce := make([]byte, s.aead.NonceSize())
			if _, err = rand.Read(nonce); err != nil {
				return s.fail(err)
			}
			data := s.aead.Seal(nonce, nonce, plain, []byte(change.Bucket+"\x00"+id))
			_, err = tx.Exec("INSERT INTO state(bucket, id, value) VALUES(?, ?, ?) ON CONFLICT(bucket, id) DO UPDATE SET value=excluded.value", change.Bucket, id, data)
		}
		if err != nil {
			return s.fail(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return s.fail(err)
	}
	return nil
}

func (s *SQLite) Close() error {
	_ = s.fail(errors.New("state store closed"))
	s.operations.Lock()
	defer s.operations.Unlock()
	// sql.DB.Close alone does not drain outstanding transactions. Retain
	// ownership until every operation has committed/rolled back or closed rows.
	if err := s.db.Close(); err != nil {
		return err
	}
	return s.lock.Close()
}

const sqliteApplicationID = 0x4b4d4350 // KMCP

// Inspect existing files without changing their permissions or journaling mode.
// A misconfigured path must not modify another application's database.
func inspectSQLite(uri string) (bool, error) {
	db, err := sql.Open("sqlite", uri+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	var version, appID int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return false, err
	}
	if err := db.QueryRow("PRAGMA application_id").Scan(&appID); err != nil {
		return false, err
	}
	if version == 0 && appID == 0 {
		var tables int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
			return false, err
		}
		if tables != 0 {
			return false, errors.New("SQLite state path points to another database")
		}
		return true, nil
	}
	if version != 1 || appID != sqliteApplicationID {
		return false, errors.New("unsupported SQLite state schema or application")
	}
	return false, nil
}
