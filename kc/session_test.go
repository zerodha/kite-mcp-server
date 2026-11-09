package kc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

func TestNewSessionManager(t *testing.T) {
	manager := NewSessionManager(testLogger())

	if manager.sessionDuration != DefaultSessionDuration {
		t.Errorf("Expected default duration of %v, got %v", DefaultSessionDuration, manager.sessionDuration)
	}

	if manager.sessions.Len() != 0 {
		t.Error("Expected empty sessions map")
	}
}

func TestGenerateSession(t *testing.T) {
	manager := NewSessionManager(testLogger())

	sessionID := manager.Generate()

	// Should be valid UUID with prefix
	if !strings.HasPrefix(sessionID, mcpSessionPrefix) {
		t.Errorf("Expected session ID to have prefix %s, got %s", mcpSessionPrefix, sessionID)
	}

	if _, err := uuid.Parse(sessionID[len(mcpSessionPrefix):]); err != nil {
		t.Errorf("Expected valid UUID after prefix, got error: %v", err)
	}

	// Should exist in sessions map
	manager.mu.RLock()
	session, exists := manager.sessions.Lookup(sessionID)
	manager.mu.RUnlock()

	if !exists {
		t.Error("Expected session to exist in manager")
	}

	if session.ID != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, session.ID)
	}

	if session.Terminated {
		t.Error("Expected new session to not be terminated")
	}

	if session.Credentials != nil {
		t.Error("Expected new session credentials to be nil")
	}
}

func TestGenerateWithData(t *testing.T) {
	manager := NewSessionManager(testLogger())
	testData := &KiteCredentials{
		AccessToken: "test-token",
		UserID:      "test-user",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	sessionID := manager.GenerateWithData(testData)

	manager.mu.RLock()
	session, exists := manager.sessions.Lookup(sessionID)
	manager.mu.RUnlock()

	if !exists {
		t.Fatal("Expected session to exist")
	}

	if session.OAuthData != testData {
		t.Error("Expected session OAuth data to match provided data")
	}
}

func TestGetOrCreate(t *testing.T) {
	manager := NewSessionManager(testLogger())

	// Test creating new session
	sessionID := "test-session-123"
	session, isNew, err := manager.GetOrCreate(sessionID)
	if err != nil {
		t.Errorf("Expected no error getting/creating session, got: %v", err)
	}

	if !isNew {
		t.Error("Expected isNew to be true for first call")
	}

	if session.ID != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, session.ID)
	}

	// Test getting existing session
	session2, isNew2, err2 := manager.GetOrCreate(sessionID)
	if err2 != nil {
		t.Errorf("Expected no error on second call, got: %v", err2)
	}

	if isNew2 {
		t.Error("Expected isNew to be false on second call")
	}

	// Since we return copies now, compare the content instead of pointer equality
	if session2.ID != session.ID {
		t.Error("Expected sessions to have the same ID")
	}

	if session2.CreatedAt != session.CreatedAt {
		t.Error("Expected sessions to have the same CreatedAt time")
	}
}

func TestUpdateCredentialsPreservesDeadlineAndCopiesCredentials(t *testing.T) {
	manager := NewSessionManagerWithDuration(testLogger(), time.Hour)
	sessionID := manager.Generate()
	before, err := manager.Get(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	creds := &KiteCredentials{AccessToken: "original", UserID: "user", ExpiresAt: time.Now().Add(time.Hour)}
	if err := manager.UpdateCredentials(sessionID, creds); err != nil {
		t.Fatal(err)
	}
	creds.AccessToken = "mutated-by-caller"
	after, err := manager.Get(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Credentials.AccessToken != "original" {
		t.Fatalf("session retained caller-owned credentials pointer")
	}
	if !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Fatalf("credential update changed grant expiry: before=%v after=%v", before.ExpiresAt, after.ExpiresAt)
	}
}

func TestValidate(t *testing.T) {
	manager := NewSessionManager(testLogger())

	// Test empty session ID
	terminated, err := manager.Validate("")
	if err == nil {
		t.Error("Expected error for empty session ID")
	}
	if !terminated {
		t.Error("Expected terminated to be true for invalid session")
	}

	// Test non-existent session
	terminated, err = manager.Validate("non-existent-session")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}
	if !terminated {
		t.Error("Expected terminated to be true for non-existent session")
	}

	// Test valid session
	sessionID := manager.Generate()
	terminated, err = manager.Validate(sessionID)
	if err != nil {
		t.Errorf("Expected no error for valid session, got: %v", err)
	}
	if terminated {
		t.Error("Expected terminated to be false for valid session")
	}
}

func TestTerminate(t *testing.T) {
	manager := NewSessionManager(testLogger())

	// Test empty session ID
	success, err := manager.Terminate("")
	if err == nil {
		t.Error("Expected error for empty session ID")
	}
	if success {
		t.Error("Expected success to be false for invalid session")
	}

	// Test non-existent session
	success, err = manager.Terminate("non-existent-session")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}
	if success {
		t.Error("Expected success to be false for non-existent session")
	}

	// Test valid session
	sessionID := manager.Generate()
	success, err = manager.Terminate(sessionID)
	if err != nil {
		t.Errorf("Expected no error for valid session, got: %v", err)
	}
	if !success {
		t.Error("Expected success to be true for valid session")
	}

	// Verify session is removed
	manager.mu.RLock()
	_, exists := manager.sessions.Lookup(sessionID)
	manager.mu.RUnlock()

	if exists {
		t.Error("Expected session to be removed after termination")
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	manager := NewSessionManager(testLogger())

	// Initially should clean 0 sessions
	cleaned := manager.CleanupExpiredSessions()
	if cleaned != 0 {
		t.Errorf("Expected 0 cleaned sessions initially, got %d", cleaned)
	}

	// Create some sessions
	manager.Generate()
	manager.Generate()

	// No sessions should be expired yet
	cleaned = manager.CleanupExpiredSessions()
	if cleaned != 0 {
		t.Errorf("Expected 0 cleaned sessions for fresh sessions, got %d", cleaned)
	}

	// Create an expired session manually
	expiredSessionID := "expired-session"
	now := time.Now()
	expiredSession := &Session{
		ID:        expiredSessionID,
		CreatedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-1 * time.Hour), // Expired 1 hour ago
	}
	manager.mu.Lock()
	manager.sessions.Set(expiredSessionID, expiredSession)
	manager.mu.Unlock()

	// Now cleanup should find the expired session
	cleaned = manager.CleanupExpiredSessions()
	if cleaned != 1 {
		t.Errorf("Expected 1 cleaned expired session, got %d", cleaned)
	}

	// Verify expired session is removed
	manager.mu.RLock()
	_, exists := manager.sessions.Lookup(expiredSessionID)
	manager.mu.RUnlock()

	if exists {
		t.Error("Expected expired session to be removed")
	}
}

func TestCleanupRoutine(t *testing.T) {
	manager := NewSessionManager(testLogger())

	// Test starting and stopping cleanup routine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.StartCleanupRoutine(ctx)
	manager.StopCleanupRoutine()

	// Should not panic
}

func TestAddCleanupHook(t *testing.T) {
	manager := NewSessionManager(testLogger())

	hookCalled := false
	hook := func(s *Session) {
		hookCalled = true
	}

	manager.AddCleanupHook(hook)

	// Create and terminate a session to trigger the hook
	sessionID := manager.Generate()
	_, err := manager.Terminate(sessionID)
	if err != nil {
		t.Errorf("Expected no error terminating session, got: %v", err)
	}

	if !hookCalled {
		t.Error("Expected cleanup hook to be called")
	}
}

func TestConcurrentSessionAccess(t *testing.T) {
	manager := NewSessionManager(testLogger())
	const numGoroutines = 100
	const sessionID = "concurrent-test-session"

	// Create a session first
	manager.Generate()

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*3)

	// Test concurrent Get operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session, err := manager.Get(sessionID)
			if err == nil && session == nil {
				errors <- fmt.Errorf("got nil session without error")
			}
			// Some will succeed (if session exists) or fail (if not found) - both are valid
		}()
	}

	// Test concurrent GetOrCreate operations with same ID
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session, isNew, err := manager.GetOrCreate(sessionID)
			if err != nil {
				errors <- fmt.Errorf("unexpected error in GetOrCreate: %v", err)
			}
			if session == nil {
				errors <- fmt.Errorf("got nil session from GetOrCreate")
			}
			// isNew can be true or false depending on timing - both valid
			_ = isNew
		}()
	}

	// Test concurrent Validate operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			terminated, err := manager.Validate(sessionID)
			// Results may vary but should not panic or cause data races
			_ = terminated
			_ = err
		}()
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Error(err)
	}
}

func TestConcurrentSessionCreation(t *testing.T) {
	manager := NewSessionManager(testLogger())
	const numGoroutines = 50

	var wg sync.WaitGroup
	sessionIDs := make(chan string, numGoroutines)

	// Create many sessions concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sessionID := manager.Generate()
			sessionIDs <- sessionID
		}()
	}

	wg.Wait()
	close(sessionIDs)

	// Verify all sessions are unique
	seen := make(map[string]bool)
	count := 0
	for sessionID := range sessionIDs {
		if seen[sessionID] {
			t.Errorf("Duplicate session ID generated: %s", sessionID)
		}
		seen[sessionID] = true
		count++
	}

	if count != numGoroutines {
		t.Errorf("Expected %d unique sessions, got %d", numGoroutines, count)
	}
}

func TestConcurrentSessionTermination(t *testing.T) {
	manager := NewSessionManager(testLogger())
	const numGoroutines = 20

	// Create multiple sessions
	sessionIDs := make([]string, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		sessionIDs[i] = manager.Generate()
	}

	var wg sync.WaitGroup
	successCount := int64(0)

	// Try to terminate all sessions concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			success, err := manager.Terminate(sessionID)
			if err == nil && success {
				atomic.AddInt64(&successCount, 1)
			}
		}(sessionIDs[i])
	}

	wg.Wait()

	// All terminations should succeed since sessions exist
	if successCount != numGoroutines {
		t.Errorf("Expected %d successful terminations, got %d", numGoroutines, successCount)
	}

	// Verify sessions are actually removed
	manager.mu.RLock()
	remaining := manager.sessions.Len()
	manager.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("Expected 0 sessions remaining, got %d", remaining)
	}
}

func TestConcurrentCleanup(t *testing.T) {
	manager := NewSessionManager(testLogger())
	const numGoroutines = 10
	const numSessions = 50

	// Create many expired sessions
	manager.mu.Lock()
	pastTime := time.Now().Add(-2 * time.Hour)
	for i := 0; i < numSessions; i++ {
		sessionID := fmt.Sprintf("expired-session-%d", i)
		manager.sessions.Set(sessionID, &Session{
			ID:        sessionID,
			CreatedAt: pastTime,
			ExpiresAt: pastTime.Add(time.Hour), // Expired 1 hour ago
		})
	}
	manager.mu.Unlock()

	var wg sync.WaitGroup
	cleanupCounts := make(chan int, numGoroutines)

	// Run cleanup concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			count := manager.CleanupExpiredSessions()
			cleanupCounts <- count
		}()
	}

	wg.Wait()
	close(cleanupCounts)

	// Total cleaned should equal the number of expired sessions
	totalCleaned := 0
	for count := range cleanupCounts {
		totalCleaned += count
	}

	if totalCleaned != numSessions {
		t.Errorf("Expected total cleaned %d, got %d", numSessions, totalCleaned)
	}

	// Verify no sessions remain
	manager.mu.RLock()
	remaining := manager.sessions.Len()
	manager.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("Expected 0 sessions remaining after cleanup, got %d", remaining)
	}
}

func TestPendingAuthorizationCapacityAndExpiryRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewSessionManager(testLogger())
		active := manager.Generate()
		if err := manager.UpdateCredentials(active, &KiteCredentials{UserID: "user", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		var first string
		for range maxPendingAuthorizations {
			id, err := manager.GeneratePendingAuthorization("pending")
			if err != nil {
				t.Fatal(err)
			}
			if first == "" {
				first = id
			}
		}
		for range 2 {
			if id, err := manager.GeneratePendingAuthorization("pending"); id != "" || !errors.Is(err, ErrPendingAuthorizationLimit) {
				t.Fatal("capacity did not reject pending flow", id, err)
			}
		}
		if _, err := manager.Get(first); err != nil {
			t.Fatal("live pending flow was evicted", err)
		}
		secondActive := manager.Generate()
		if manager.pendingAuth.Len() != maxPendingAuthorizations || manager.sessions.Len() != maxPendingAuthorizations+2 {
			t.Fatal("capacity rejection changed session store")
		}
		if _, err := manager.Terminate(first); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Minute)
		livePending, err := manager.GeneratePendingAuthorization("live pending")
		if err != nil {
			t.Fatal("terminating a flow did not release its slot", err)
		}
		time.Sleep(5 * time.Minute)
		if _, err := manager.GeneratePendingAuthorization("new pending"); err != nil {
			t.Fatal("expired flows did not release capacity", err)
		}
		if manager.pendingAuth.Len() != 2 || manager.sessions.Len() != 4 {
			t.Fatal("expiry recovery removed wrong sessions", manager.pendingAuth.Len(), manager.sessions.Len())
		}
		for _, id := range []string{active, secondActive, livePending} {
			if _, err := manager.Get(id); err != nil {
				t.Fatal("live session was evicted", id, err)
			}
		}
	})
}

func TestPendingAuthorizationAdmissionIsAtomic(t *testing.T) {
	manager := NewSessionManager(testLogger())
	for range maxPendingAuthorizations - 10 {
		if _, err := manager.GeneratePendingAuthorization("pending"); err != nil {
			t.Fatal(err)
		}
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			_, err := manager.GeneratePendingAuthorization("pending")
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrPendingAuthorizationLimit) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 10 || manager.pendingAuth.Len() != maxPendingAuthorizations || manager.sessions.Len() != maxPendingAuthorizations {
		t.Fatal("concurrent admission exceeded capacity", accepted.Load())
	}
}

func TestPendingAuthorizationDeletionReleasesAdmission(t *testing.T) {
	for _, operation := range []string{"terminate", "validate", "cleanup", "get-or-create"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				manager := NewSessionManager(testLogger())
				id, err := manager.GeneratePendingAuthorization("pending")
				if err != nil {
					t.Fatal(err)
				}
				time.Sleep(DefaultPendingSessionDuration + time.Second)
				switch operation {
				case "terminate":
					_, err = manager.Terminate(id)
				case "validate":
					_, err = manager.Validate(id)
				case "cleanup":
					manager.CleanupExpiredSessions()
				case "get-or-create":
					_, _, err = manager.GetOrCreate(id)
				}
				if err != nil {
					t.Fatal(err)
				}
				if manager.pendingAuth.Len() != 0 {
					t.Fatal("deleted authorization still occupies admission slot")
				}
			})
		})
	}
}
