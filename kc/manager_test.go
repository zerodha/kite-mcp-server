package kc

import (
	"io"
	"log/slog"
	"testing"

	"github.com/zerodha/kite-mcp-server/kc/instruments"
)

// newTestInstrumentsManager creates a fast test instruments manager without HTTP calls
func newTestInstrumentsManager() *instruments.Manager {
	// Create test data
	testInsts := []*instruments.Instrument{
		{
			ID:              "NSE:SBIN",
			InstrumentToken: 779521,
			ExchangeToken:   3045,
			Tradingsymbol:   "SBIN",
			Exchange:        "NSE",
			ISIN:            "INE062A01020",
			Name:            "STATE BANK OF INDIA",
			InstrumentType:  "EQ",
			Segment:         "NSE",
			Active:          true,
		},
		{
			ID:              "NSE:RELIANCE",
			InstrumentToken: 738561,
			ExchangeToken:   2885,
			Tradingsymbol:   "RELIANCE",
			Exchange:        "NSE",
			ISIN:            "INE002A01018",
			Name:            "RELIANCE INDUSTRIES LIMITED",
			InstrumentType:  "EQ",
			Segment:         "NSE",
			Active:          true,
		},
	}

	// Create test data map
	testMap := make(map[uint32]*instruments.Instrument)
	for _, inst := range testInsts {
		testMap[inst.InstrumentToken] = inst
	}

	// Create manager with test data (automatically skips HTTP calls)
	config := instruments.DefaultUpdateConfig()
	config.EnableScheduler = false

	manager, err := instruments.New(instruments.Config{
		UpdateConfig: config,
		Logger:       testLogger(),
		TestData:     testMap,
	})
	if err != nil {
		panic("failed to create test instruments manager: " + err.Error())
	}

	return manager
}

// testLogger creates a discard logger for tests
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestManager creates a test manager with provided instruments manager
func newTestManager(apiKey, apiSecret string) (*Manager, error) {
	return New(Config{
		APIKey:             apiKey,
		APISecret:          apiSecret,
		InstrumentsManager: newTestInstrumentsManager(),
		Logger:             testLogger(),
	})
}

func TestNewManager(t *testing.T) {
	apiKey := "test_key"
	apiSecret := "test_secret"

	manager, err := newTestManager(apiKey, apiSecret)
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	if manager == nil {
		t.Fatal("Expected non-nil manager")
	}

	if manager.apiKey != apiKey {
		t.Errorf("Expected API key %s, got %s", apiKey, manager.apiKey)
	}

	if manager.apiSecret != apiSecret {
		t.Errorf("Expected API secret %s, got %s", apiSecret, manager.apiSecret)
	}

	// Verify session signer is initialized
	if manager.sessionSigner == nil {
		t.Error("Expected session signer to be initialized")
	}

	if manager.Instruments == nil {
		t.Error("Expected instruments manager to be initialized")
	}

	if manager.sessionManager == nil {
		t.Error("Expected session registry to be initialized")
	}

	if manager.templates == nil {
		t.Error("Expected templates to be initialized")
	}
}

// KiteConnect API Tests (consolidated from api_test.go)

func TestNewKiteConnect(t *testing.T) {
	apiKey := "test_api_key"

	kc := NewKiteConnect(apiKey)

	if kc == nil {
		t.Fatal("Expected non-nil KiteConnect")
	}

	if kc.Client == nil {
		t.Error("Expected non-nil Client")
	}
}

func TestManagerGenerateSession(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	sessionID := manager.GenerateSession()

	if sessionID == "" {
		t.Error("Expected non-empty session ID")
	}

	// Verify session exists in session manager
	sessionData, err := manager.GetSession(sessionID)
	if err != nil {
		t.Errorf("Expected session to exist, got error: %v", err)
	}

	if sessionData == nil {
		t.Error("Expected non-nil session data")
		return
	}

	if sessionData.Kite == nil {
		t.Error("Expected Kite client to be initialized")
	}
}

func TestManagerGetSession(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Test empty session ID
	_, err = manager.GetSession("")
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound for empty session ID, got: %v", err)
	}

	// Test non-existent session
	_, err = manager.GetSession("non-existent-session")
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound for non-existent session, got: %v", err)
	}

	// Test valid session
	sessionID := manager.GenerateSession()
	sessionData, err := manager.GetSession(sessionID)
	if err != nil {
		t.Errorf("Expected no error for valid session, got: %v", err)
	}

	if sessionData == nil {
		t.Error("Expected non-nil session data")
	}

	// Test terminated session
	manager.ClearSession(sessionID)
	_, err = manager.GetSession(sessionID)
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound for terminated session, got: %v", err)
	}
}

func TestClearSession(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Test empty session ID (should not panic)
	manager.ClearSession("")

	// Test valid session
	sessionID := manager.GenerateSession()

	// Verify session exists
	_, err = manager.GetSession(sessionID)
	if err != nil {
		t.Errorf("Expected session to exist before clearing, got error: %v", err)
	}

	// Clear session
	manager.ClearSession(sessionID)

	// Verify session is cleared
	_, err = manager.GetSession(sessionID)
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound after clearing session, got: %v", err)
	}
}

func TestSessionLoginURL(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Test empty session ID
	_, err = manager.SessionLoginURL("")
	if err != ErrInvalidSessionID {
		t.Errorf("Expected ErrInvalidSessionID for empty session ID, got: %v", err)
	}

	// Test non-existent session
	_, err = manager.SessionLoginURL("non-existent-session")
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound for non-existent session, got: %v", err)
	}

	// Test valid session
	sessionID := manager.GenerateSession()
	loginURL, err := manager.SessionLoginURL(sessionID)
	if err != nil {
		t.Errorf("Expected no error for valid session, got: %v", err)
	}

	if loginURL == "" {
		t.Error("Expected non-empty login URL")
	}

	if !managerContains(loginURL, "session_id%3D"+sessionID) {
		t.Errorf("Expected login URL to contain URL-encoded session ID. URL: %s, SessionID: %s", loginURL, sessionID)
	}
}

func TestCompleteSession(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Test empty session ID
	err = manager.CompleteSession("", "test_token")
	if err != ErrInvalidSessionID {
		t.Errorf("Expected ErrInvalidSessionID for empty session ID, got: %v", err)
	}

	// Test non-existent session
	err = manager.CompleteSession("non-existent-session", "test_token")
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound for non-existent session, got: %v", err)
	}

	// Test valid session with invalid token (will fail at Kite API level)
	sessionID := manager.GenerateSession()
	err = manager.CompleteSession(sessionID, "invalid_token")
	if err == nil {
		t.Error("Expected error for invalid request token")
	}
}

func TestGetActiveSessionCount(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Initially should be 0
	count := manager.GetActiveSessionCount()
	if count != 0 {
		t.Errorf("Expected 0 active sessions initially, got %d", count)
	}

	// Create sessions
	id1 := manager.GenerateSession()
	id2 := manager.GenerateSession()

	count = manager.GetActiveSessionCount()
	if count != 2 {
		t.Errorf("Expected 2 active sessions, got %d", count)
	}

	// Clear one session
	manager.ClearSession(id1)

	count = manager.GetActiveSessionCount()
	if count != 1 {
		t.Errorf("Expected 1 active session after clearing one, got %d", count)
	}

	// Clear remaining session
	manager.ClearSession(id2)

	count = manager.GetActiveSessionCount()
	if count != 0 {
		t.Errorf("Expected 0 active sessions after clearing all, got %d", count)
	}
}

func TestManagerCleanupExpiredSessions(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Initially should clean 0 sessions
	cleaned := manager.CleanupExpiredSessions()
	if cleaned != 0 {
		t.Errorf("Expected 0 cleaned sessions initially, got %d", cleaned)
	}

	// Create some sessions
	manager.GenerateSession()
	manager.GenerateSession()

	// No sessions should be expired yet
	cleaned = manager.CleanupExpiredSessions()
	if cleaned != 0 {
		t.Errorf("Expected 0 cleaned sessions for fresh sessions, got %d", cleaned)
	}
}

func TestSessionManager(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	sessionManager := manager.SessionManager()
	if sessionManager == nil {
		t.Error("Expected non-nil session registry")
	}

	// Verify it's the same instance
	if sessionManager != manager.sessionManager {
		t.Error("Expected returned session manager to be the same instance")
	}
}

func TestStopCleanupRoutine(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Should not panic
	manager.StopCleanupRoutine()
}

func TestGetOrCreateSession(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	sessionID := manager.GenerateSession()

	// Clear the data from the session to force creation of new data
	err = manager.sessionManager.UpdateSessionData(sessionID, nil)
	if err != nil {
		t.Fatalf("Failed to clear session data: %v", err)
	}

	// Test getting/creating session for the first time after clearing data
	kiteData, isNew, err := manager.GetOrCreateSession(sessionID)
	if err != nil {
		t.Errorf("Expected no error getting/creating session, got: %v", err)
	}

	if !isNew {
		t.Error("Expected isNew to be true for first call")
	}

	if kiteData == nil {
		t.Error("Expected non-nil KiteSessionData")
	}

	// Test getting the same session again
	kiteData2, isNew2, err := manager.GetOrCreateSession(sessionID)
	if err != nil {
		t.Errorf("Expected no error on second call, got: %v", err)
	}

	if isNew2 {
		t.Error("Expected isNew to be false on second call")
	}

	if kiteData2 == nil {
		t.Error("Expected non-nil KiteSessionData on second call")
	}
}

// TestNewConfigConstructor tests the new Config-based constructor
func TestNewConfigConstructor(t *testing.T) {
	// Test minimal config
	t.Run("minimal_config", func(t *testing.T) {
		manager, err := New(Config{
			APIKey:             "test_key",
			APISecret:          "test_secret",
			InstrumentsManager: newTestInstrumentsManager(),
			Logger:             testLogger(),
		})
		if err != nil {
			t.Fatalf("Expected no error with minimal config, got: %v", err)
		}

		if manager.apiKey != "test_key" {
			t.Errorf("Expected API key 'test_key', got %s", manager.apiKey)
		}
		if manager.apiSecret != "test_secret" {
			t.Errorf("Expected API secret 'test_secret', got %s", manager.apiSecret)
		}
		if manager.Instruments == nil {
			t.Error("Expected instruments manager to be set")
		}
		if manager.sessionSigner == nil {
			t.Error("Expected session signer to be initialized")
		}
	})

	// Test validation
	t.Run("validation", func(t *testing.T) {
		// Missing API key
		_, err := New(Config{
			APISecret: "test_secret",
			Logger:    testLogger(),
		})
		if err == nil || err.Error() != "APIKey is required" {
			t.Errorf("Expected 'APIKey is required' error, got: %v", err)
		}

		// Missing API secret
		_, err = New(Config{
			APIKey: "test_key",
			Logger: testLogger(),
		})
		if err == nil || err.Error() != "APISecret is required" {
			t.Errorf("Expected 'APISecret is required' error, got: %v", err)
		}

		// Missing logger
		_, err = New(Config{
			APIKey:    "test_key",
			APISecret: "test_secret",
		})
		if err == nil || err.Error() != "logger is required" {
			t.Errorf("Expected 'logger is required' error, got: %v", err)
		}
	})

	// Test with custom session signer
	t.Run("custom_session_signer", func(t *testing.T) {
		customSigner := NewSessionSignerWithKey([]byte("test-key-32-bytes-long-for-hmac"))

		manager, err := New(Config{
			APIKey:             "test_key",
			APISecret:          "test_secret",
			InstrumentsManager: newTestInstrumentsManager(),
			SessionSigner:      customSigner,
			Logger:             testLogger(),
		})
		if err != nil {
			t.Fatalf("Expected no error with custom session signer, got: %v", err)
		}

		if manager.sessionSigner != customSigner {
			t.Error("Expected custom session signer to be used")
		}
	})
}

// TestExternalSessionIDFromErrorLog tests the exact session ID from the error log
func TestExternalSessionIDFromErrorLog(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// This is the exact session ID from the error log that was failing
	externalSessionID := "6f615000-2644-45a7-a27c-f579e20b5992"

	// Should be able to get or create session with external session ID
	kiteSession, isNew, err := manager.GetOrCreateSession(externalSessionID)
	if err != nil {
		t.Errorf("Expected no error for external session ID from error log, got: %v", err)
	}
	if !isNew {
		t.Error("Expected new session to be created for external session ID")
	}
	if kiteSession == nil {
		t.Error("Expected non-nil Kite session data")
	} else if kiteSession.Kite == nil {
		t.Error("Expected Kite client to be initialized")
	}

	// Subsequent call should reuse existing session
	kiteSession2, isNew2, err2 := manager.GetOrCreateSession(externalSessionID)
	if err2 != nil {
		t.Errorf("Expected no error on second call, got: %v", err2)
	}
	if isNew2 {
		t.Error("Expected existing session to be reused")
	}
	if kiteSession2 != kiteSession {
		t.Error("Expected same session instance to be returned")
	}
}

// Helper function to check if string contains substring
func managerContains(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
