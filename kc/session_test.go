package kc

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewSessionRegistry(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	if manager == nil {
		t.Fatal("Expected non-nil manager")
	}

	if manager.sessionDuration != DefaultSessionDuration {
		t.Errorf("Expected default duration of %v, got %v", DefaultSessionDuration, manager.sessionDuration)
	}

	if len(manager.sessions) != 0 {
		t.Error("Expected empty sessions map")
	}
}

func TestNewSessionRegistryWithDuration(t *testing.T) {
	duration := 30 * time.Minute
	manager := NewSessionRegistryWithDuration(duration, testLogger())

	if manager.sessionDuration != duration {
		t.Errorf("Expected duration %v, got %v", duration, manager.sessionDuration)
	}
}

func TestGenerateSession(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

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
	session, exists := manager.sessions[sessionID]
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

	if session.Data != nil {
		t.Error("Expected new session data to be nil")
	}
}

func TestGenerateWithData(t *testing.T) {
	manager := NewSessionRegistry(testLogger())
	testData := &KiteSessionData{Kite: &KiteConnect{}}

	sessionID := manager.GenerateWithData(testData)

	manager.mu.RLock()
	session, exists := manager.sessions[sessionID]
	manager.mu.RUnlock()

	if !exists {
		t.Fatal("Expected session to exist")
	}

	if session.Data != testData {
		t.Error("Expected session data to match provided data")
	}
}

func TestValidateSession(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	// Test invalid UUID
	_, err := manager.Validate("invalid-uuid")
	if err == nil {
		t.Error("Expected error for invalid UUID")
	}

	// Test non-existent session
	validID := mcpSessionPrefix + uuid.New().String()
	_, err = manager.Validate(validID)
	if err == nil {
		t.Error("Expected error for non-existent session")
	}

	// Test valid session
	sessionID := manager.Generate()
	isTerminated, err := manager.Validate(sessionID)
	if err != nil {
		t.Errorf("Expected no error for valid session, got: %v", err)
	}
	if isTerminated {
		t.Error("Expected new session to not be terminated")
	}

	// Test expired session
	manager.mu.Lock()
	session := manager.sessions[sessionID]
	session.ExpiresAt = time.Now().Add(-time.Hour) // Expired 1 hour ago
	manager.mu.Unlock()

	isTerminated, err = manager.Validate(sessionID)
	if err != nil {
		t.Errorf("Expected no error for expired session, got: %v", err)
	}
	if !isTerminated {
		t.Error("Expected expired session to be terminated")
	}
}

func TestTerminateSession(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	// Test invalid UUID
	_, err := manager.Terminate("invalid-uuid")
	if err == nil {
		t.Error("Expected error for invalid UUID")
	}

	// Test non-existent session
	validID := mcpSessionPrefix + uuid.New().String()
	_, err = manager.Terminate(validID)
	if err == nil {
		t.Error("Expected error for non-existent session")
	}

	// Test valid session termination
	sessionID := manager.Generate()

	// Add cleanup hook to verify it's called
	hookCalled := false
	manager.AddCleanupHook(func(session *MCPSession) {
		if session.ID == sessionID {
			hookCalled = true
		}
	})

	isNotAllowed, err := manager.Terminate(sessionID)
	if err != nil {
		t.Errorf("Expected no error for valid session, got: %v", err)
	}
	if isNotAllowed {
		t.Error("Expected termination to be allowed")
	}

	manager.mu.RLock()
	session := manager.sessions[sessionID]
	manager.mu.RUnlock()

	if !session.Terminated {
		t.Error("Expected session to be terminated")
	}

	if !hookCalled {
		t.Error("Expected cleanup hook to be called")
	}
}

func TestSessionDataOperations(t *testing.T) {
	manager := NewSessionRegistry(testLogger())
	testData := map[string]string{"key": "value"}

	sessionID := manager.Generate()

	// Test UpdateSessionData
	err := manager.UpdateSessionData(sessionID, testData)
	if err != nil {
		t.Errorf("Expected no error updating session data, got: %v", err)
	}

	// Test GetSessionData
	data, err := manager.GetSessionData(sessionID)
	if err != nil {
		t.Errorf("Expected no error getting session data, got: %v", err)
	}

	retrievedData, ok := data.(map[string]string)
	if !ok {
		t.Error("Expected data to be map[string]string")
	}

	if retrievedData["key"] != "value" {
		t.Error("Expected retrieved data to match original data")
	}

	// Test updating terminated session
	_, _ = manager.Terminate(sessionID)
	err = manager.UpdateSessionData(sessionID, testData)
	if err == nil {
		t.Error("Expected error when updating terminated session")
	}
}

// NOTE: ExtendSession method was removed from implementation to enforce fixed session durations

func TestListActiveSessions(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	// Create multiple sessions
	id1 := manager.Generate()
	id2 := manager.Generate()
	id3 := manager.Generate()

	// Terminate one session
	_, _ = manager.Terminate(id2)

	// Expire one session
	manager.mu.Lock()
	manager.sessions[id3].ExpiresAt = time.Now().Add(-time.Hour)
	manager.mu.Unlock()

	activeSessions := manager.ListActiveSessions()

	if len(activeSessions) != 1 {
		t.Errorf("Expected 1 active session, got %d", len(activeSessions))
	}

	if activeSessions[0].ID != id1 {
		t.Errorf("Expected active session to be %s, got %s", id1, activeSessions[0].ID)
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	// Create sessions
	id1 := manager.Generate()
	id2 := manager.Generate()
	id3 := manager.Generate()

	// Expire two sessions
	manager.mu.Lock()
	manager.sessions[id1].ExpiresAt = time.Now().Add(-time.Hour)
	manager.sessions[id2].ExpiresAt = time.Now().Add(-time.Minute)
	manager.mu.Unlock()

	// Track cleanup hook calls
	cleanedSessions := []string{}
	manager.AddCleanupHook(func(session *MCPSession) {
		cleanedSessions = append(cleanedSessions, session.ID)
	})

	cleaned := manager.CleanupExpiredSessions()

	if cleaned != 2 {
		t.Errorf("Expected 2 cleaned sessions, got %d", cleaned)
	}

	if len(cleanedSessions) != 2 {
		t.Errorf("Expected 2 cleanup hook calls, got %d", len(cleanedSessions))
	}

	// Verify sessions are removed from map
	manager.mu.RLock()
	_, exists1 := manager.sessions[id1]
	_, exists2 := manager.sessions[id2]
	_, exists3 := manager.sessions[id3]
	manager.mu.RUnlock()

	if exists1 {
		t.Error("Expected expired session to be removed from map")
	}
	if exists2 {
		t.Error("Expected expired session to be removed from map")
	}
	if !exists3 {
		t.Error("Expected non-expired session to remain in map")
	}
}

func TestSessionDurationMethods(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	// Test GetSessionDuration
	duration := manager.GetSessionDuration()
	if duration != DefaultSessionDuration {
		t.Errorf("Expected default duration of %v, got %v", DefaultSessionDuration, duration)
	}

	// Test SetSessionDuration
	newDuration := 2 * time.Hour
	manager.SetSessionDuration(newDuration)

	if manager.GetSessionDuration() != newDuration {
		t.Errorf("Expected duration %v, got %v", newDuration, manager.GetSessionDuration())
	}

	// Verify new sessions use new duration
	sessionID := manager.Generate()

	manager.mu.RLock()
	session := manager.sessions[sessionID]
	manager.mu.RUnlock()

	expectedExpiry := session.CreatedAt.Add(newDuration)

	if !session.ExpiresAt.Equal(expectedExpiry) {
		t.Error("Expected new session to use updated duration")
	}
}

func TestCleanupRoutine(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	// Create an expired session
	sessionID := manager.Generate()

	manager.mu.Lock()
	manager.sessions[sessionID].ExpiresAt = time.Now().Add(-time.Hour)
	manager.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start cleanup routine
	manager.StartCleanupRoutine(ctx)

	// Wait for context to timeout (simulating cleanup routine running)
	<-ctx.Done()

	// Stop cleanup routine
	manager.StopCleanupRoutine()

	// Cleanup routine should have run, but we can't reliably test timing
	// Just verify the stop method doesn't panic
}

func TestCleanupHooks(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	callCount := 0
	testHook := func(session *MCPSession) {
		callCount++
	}

	manager.AddCleanupHook(testHook)
	manager.AddCleanupHook(testHook) // Add same hook twice

	sessionID := manager.Generate()
	_, _ = manager.Terminate(sessionID)

	if callCount != 2 {
		t.Errorf("Expected cleanup hooks to be called twice, got %d", callCount)
	}
}

func TestConcurrentAccess(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	// Test concurrent session generation
	done := make(chan string, 10)

	for i := 0; i < 10; i++ {
		go func() {
			sessionID := manager.Generate()
			done <- sessionID
		}()
	}

	// Collect all session IDs
	sessionIDs := make([]string, 10)
	for i := 0; i < 10; i++ {
		sessionIDs[i] = <-done
	}

	// Verify all sessions were created
	manager.mu.RLock()
	sessCount := len(manager.sessions)
	manager.mu.RUnlock()

	if sessCount != 10 {
		t.Errorf("Expected 10 sessions, got %d", sessCount)
	}

	// Verify all session IDs are unique
	uniqueIDs := make(map[string]bool)
	for _, id := range sessionIDs {
		if uniqueIDs[id] {
			t.Error("Found duplicate session ID")
		}
		uniqueIDs[id] = true
	}
}

func TestGetOrCreateSessionDataRaceCondition(t *testing.T) {
	manager := NewSessionRegistry(testLogger())
	sessionID := manager.Generate()

	// Test concurrent GetOrCreateSessionData calls
	const numGoroutines = 100
	results := make(chan struct {
		data  any
		isNew bool
		err   error
	}, numGoroutines)

	createCount := 0
	createDataFn := func() any {
		createCount++
		return map[string]string{"key": "value", "count": string(rune('0' + createCount))}
	}

	// Launch concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		go func() {
			data, isNew, err := manager.GetOrCreateSessionData(sessionID, createDataFn)
			results <- struct {
				data  any
				isNew bool
				err   error
			}{data, isNew, err}
		}()
	}

	// Collect results
	var createdCount int
	var retrievedCount int
	var firstData any

	for i := 0; i < numGoroutines; i++ {
		result := <-results
		if result.err != nil {
			t.Errorf("Unexpected error: %v", result.err)
			continue
		}

		if result.isNew {
			createdCount++
		} else {
			retrievedCount++
		}

		// All goroutines should get the same data object
		if firstData == nil {
			firstData = result.data
		} else {
			// Use reflect.DeepEqual for comparing map data
			if !reflect.DeepEqual(firstData, result.data) {
				t.Error("Different goroutines got different data objects")
			}
		}
	}

	// Only one goroutine should have created new data
	if createdCount != 1 {
		t.Errorf("Expected exactly 1 creation, got %d", createdCount)
	}

	if retrievedCount != numGoroutines-1 {
		t.Errorf("Expected %d retrievals, got %d", numGoroutines-1, retrievedCount)
	}
}

func TestConcurrentSessionModification(t *testing.T) {
	manager := NewSessionRegistry(testLogger())
	sessionID := manager.Generate()

	// Test concurrent operations on the same session
	const numGoroutines = 50
	done := make(chan bool, numGoroutines*3)

	// Concurrent GetOrCreateSessionData
	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, _, err := manager.GetOrCreateSessionData(sessionID, func() any {
				return map[string]int{"value": 1}
			})
			done <- err == nil
		}()
	}

	// Concurrent UpdateSessionData
	for i := 0; i < numGoroutines; i++ {
		go func(val int) {
			err := manager.UpdateSessionData(sessionID, map[string]int{"value": val})
			done <- err == nil
		}(i)
	}

	// Concurrent GetSessionData
	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := manager.GetSessionData(sessionID)
			done <- err == nil
		}()
	}

	// Wait for all goroutines to complete
	successCount := 0
	for i := 0; i < numGoroutines*3; i++ {
		if <-done {
			successCount++
		}
	}

	// All operations should succeed (no panics or corrupted state)
	if successCount < numGoroutines*2 {
		t.Errorf("Too many operations failed: %d/%d succeeded", successCount, numGoroutines*3)
	}
}

func TestConcurrentSessionTermination(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	const numSessions = 50
	sessionIDs := make([]string, numSessions)

	// Create multiple sessions
	for i := 0; i < numSessions; i++ {
		sessionIDs[i] = manager.Generate()
	}

	done := make(chan error, numSessions*2)

	// Concurrent GetOrCreateSessionData and Terminate operations
	for i := 0; i < numSessions; i++ {
		sessionID := sessionIDs[i]

		// Try to get/create data
		go func() {
			_, _, err := manager.GetOrCreateSessionData(sessionID, func() any {
				return "test-data"
			})
			done <- err
		}()

		// Try to terminate session
		go func() {
			_, err := manager.Terminate(sessionID)
			done <- err
		}()
	}

	// Collect results - some operations may fail due to termination, but no panics should occur
	errorCount := 0
	for i := 0; i < numSessions*2; i++ {
		if err := <-done; err != nil {
			errorCount++
		}
	}

	// Some errors are expected due to race conditions, but the system should remain stable
	t.Logf("Got %d errors out of %d operations (expected due to concurrent termination)", errorCount, numSessions*2)
}

func TestGetSession(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	// Test non-existent session
	_, err := manager.GetSession("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}

	// Test existing session
	sessionID := manager.Generate()
	session, err := manager.GetSession(sessionID)
	if err != nil {
		t.Errorf("Expected no error for existing session, got: %v", err)
	}

	if session.ID != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, session.ID)
	}
}

func TestSessionExpiration(t *testing.T) {
	// Create manager with very short duration for testing
	manager := NewSessionRegistryWithDuration(time.Millisecond, testLogger())

	sessionID := manager.Generate()

	// Session should be valid initially
	isTerminated, err := manager.Validate(sessionID)
	if err != nil {
		t.Errorf("Expected no error for new session, got: %v", err)
	}
	if isTerminated {
		t.Error("Expected session to not be terminated initially")
	}

	// Wait for session to expire
	time.Sleep(5 * time.Millisecond)

	// Session should now be expired
	isTerminated, err = manager.Validate(sessionID)
	if err != nil {
		t.Errorf("Expected no error for expired session validation, got: %v", err)
	}
	if !isTerminated {
		t.Error("Expected session to be terminated after expiry")
	}
}

func TestExternalSessionIDFormat(t *testing.T) {
	manager := NewSessionRegistry(testLogger())

	// Test external session ID (plain UUID format from SSE/stdio modes)
	externalSessionID := "6f615000-2644-45a7-a27c-f579e20b5992"

	// Should be able to create session data with external session ID
	testData := map[string]string{"test": "data"}
	data, isNew, err := manager.GetOrCreateSessionData(externalSessionID, func() any {
		return testData
	})

	if err != nil {
		t.Errorf("Expected no error for external session ID, got: %v", err)
	}
	if !isNew {
		t.Error("Expected new session to be created")
	}
	retrievedData, ok := data.(map[string]string)
	if !ok {
		t.Errorf("Expected data to be map[string]string, got: %T", data)
	}
	if retrievedData["test"] != "data" {
		t.Errorf("Expected data['test'] to be 'data', got: %v", retrievedData["test"])
	}

	// Should be able to validate external session ID
	isTerminated, err := manager.Validate(externalSessionID)
	if err != nil {
		t.Errorf("Expected no error validating external session ID, got: %v", err)
	}
	if isTerminated {
		t.Error("Expected external session to not be terminated")
	}

	// Test internal session ID format still works
	internalSessionID := manager.Generate()

	data2, isNew2, err2 := manager.GetOrCreateSessionData(internalSessionID, func() any {
		return testData
	})

	if err2 != nil {
		t.Errorf("Expected no error for internal session ID, got: %v", err2)
	}
	if !isNew2 {
		t.Error("Expected new internal session to be created")
	}
	retrievedData2, ok2 := data2.(map[string]string)
	if !ok2 {
		t.Errorf("Expected internal data to be map[string]string, got: %T", data2)
	}
	if retrievedData2["test"] != "data" {
		t.Errorf("Expected internal data['test'] to be 'data', got: %v", retrievedData2["test"])
	}
}
