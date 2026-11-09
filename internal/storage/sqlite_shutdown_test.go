package storage

import (
	"bytes"
	"crypto/cipher"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Pause a real operation after it has acquired its SQL transaction or rows.
// This reproduces the shutdown overlap without depending on disk timing.
type pausedOperationAEAD struct {
	cipher.AEAD
	entered, release chan struct{}
	read             bool
}

func (p pausedOperationAEAD) Seal(dst, nonce, plaintext, additional []byte) []byte {
	if !p.read {
		close(p.entered)
		<-p.release
	}
	return p.AEAD.Seal(dst, nonce, plaintext, additional)
}

func (p pausedOperationAEAD) Open(dst, nonce, ciphertext, additional []byte) ([]byte, error) {
	if p.read {
		close(p.entered)
		<-p.release
	}
	return p.AEAD.Open(dst, nonce, ciphertext, additional)
}

func TestSQLiteCloseDrainsOperationsBeforeReleasingOwnership(t *testing.T) {
	for _, operation := range []string{"Apply", "Load"} {
		t.Run(operation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			key := bytes.Repeat([]byte{7}, 32)
			first := openTestSQLite(t, path)
			require.NoError(t, Put(first, "permissions", "grant", true))
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			resume := func() { releaseOnce.Do(func() { close(release) }) }
			defer resume()
			first.aead = pausedOperationAEAD{first.aead, entered, release, operation == "Load"}
			opDone := make(chan error, 1)
			go func() {
				if operation == "Apply" {
					opDone <- Put(first, "permissions", "grant", false)
				} else {
					_, err := first.Load("permissions")
					opDone <- err
				}
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("operation did not start")
			}
			closed := make(chan error, 1)
			go func() { closed <- first.Close() }()
			require.Eventually(t, func() bool { return first.Check() != nil }, 3*time.Second, time.Millisecond)
			select {
			case err := <-closed:
				t.Fatalf("Close returned with an operation still active: %v", err)
			case <-time.After(25 * time.Millisecond):
			}
			second, err := OpenSQLite(path, key)
			if second != nil {
				_ = second.Close()
			}
			require.ErrorContains(t, err, "owned", "replacement must wait for the old operation")
			resume()
			select {
			case err := <-opDone:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("operation did not finish")
			}
			select {
			case err := <-closed:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("Close did not finish after the operation drained")
			}
			require.ErrorIs(t, Put(first, "permissions", "grant", true), ErrUnavailable)
			_, err = first.Load("permissions")
			require.ErrorIs(t, err, ErrUnavailable)
			second = openTestSQLite(t, path)
			values, err := second.Load("permissions")
			require.NoError(t, err)
			want := "true"
			if operation == "Apply" {
				want = "false"
			}
			require.Equal(t, want, string(values["grant"]), "replacement must restore the completed operation's state")
		})
	}
}
