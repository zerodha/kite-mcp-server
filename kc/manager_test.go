package kc

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
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
		APIKey:      apiKey,
		APISecret:   apiSecret,
		Instruments: newTestInstrumentsManager(),
		Logger:      testLogger(),
	})
}

func TestNewManager(t *testing.T) {
	apiKey := "test_key"
	apiSecret := "test_secret"

	manager, err := newTestManager(apiKey, apiSecret)
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
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
		t.Error("Expected session manager to be initialized")
	}

	if manager.templates == nil {
		t.Error("Expected templates to be initialized")
	}
}

// OAuth-based tests

func TestGetAuthenticatedClient(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Test empty session ID
	_, err = manager.GetAuthenticatedClient("")
	if err == nil {
		t.Error("Expected error for empty session ID")
	}

	// Test non-existent session
	_, err = manager.GetAuthenticatedClient("non-existent-session")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}

	// We can't easily test successful authentication without Kite API credentials
	// The main functionality is verified in integration tests
}

func TestRenderAuthorizeInterstitial(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	recorder := httptest.NewRecorder()
	err = manager.RenderAuthorizeInterstitial(recorder, "https://kite.example/login")
	if err != nil {
		t.Fatalf("Expected no error rendering interstitial, got: %v", err)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "Continue to Kite") {
		t.Error("Expected interstitial body to contain heading")
	}
	if !strings.Contains(body, "AI systems are unpredictable and non-deterministic") {
		t.Error("Expected interstitial body to contain disclaimer")
	}
	if !strings.Contains(body, "https://kite.example/login") {
		t.Error("Expected interstitial body to contain login URL")
	}
}

func TestGenerateLoginURL(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	sessionID := "test-session-id"
	url, err := manager.GenerateLoginURL(sessionID)
	if err != nil {
		t.Errorf("Expected no error generating login URL, got: %v", err)
	}

	if url == "" {
		t.Error("Expected non-empty login URL")
	}

	// URL should contain the API key
	if !strings.Contains(url, "test_key") {
		t.Error("Expected login URL to contain API key")
	}
}

func TestCompleteLogin(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Test with invalid request token (will fail API call)
	_, err = manager.CompleteLogin("invalid-token")
	if err == nil {
		t.Error("Expected error for invalid request token")
	}

	// We can't test successful login without valid Kite API credentials
}

func TestSessionManager(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	sessionManager := manager.SessionManager()
	if sessionManager == nil {
		t.Error("Expected non-nil session manager")
	}

	// Test session generation
	sessionID := sessionManager.Generate()
	if sessionID == "" {
		t.Error("Expected non-empty session ID")
	}
}

func TestShutdown(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	// Should not panic
	manager.Shutdown()
}

func TestConfigValidation(t *testing.T) {
	// Test missing API key
	_, err := New(Config{
		APISecret:   "test_secret",
		Instruments: newTestInstrumentsManager(),
		Logger:      testLogger(),
	})
	if err == nil || err.Error() != "APIKey is required" {
		t.Errorf("Expected 'APIKey is required' error, got: %v", err)
	}

	// Test missing API secret
	_, err = New(Config{
		APIKey:      "test_key",
		Instruments: newTestInstrumentsManager(),
		Logger:      testLogger(),
	})
	if err == nil || err.Error() != "APISecret is required" {
		t.Errorf("Expected 'APISecret is required' error, got: %v", err)
	}

	// Test missing logger
	_, err = New(Config{
		APIKey:      "test_key",
		APISecret:   "test_secret",
		Instruments: newTestInstrumentsManager(),
	})
	if err == nil || err.Error() != "logger is required" {
		t.Errorf("Expected 'logger is required' error, got: %v", err)
	}

	// Test missing instruments
	_, err = New(Config{
		APIKey:    "test_key",
		APISecret: "test_secret",
		Logger:    testLogger(),
	})
	if err == nil || err.Error() != "instruments manager is required" {
		t.Errorf("Expected 'instruments manager is required' error, got: %v", err)
	}
}

func TestSessionSigner(t *testing.T) {
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatalf("Expected no error creating manager, got: %v", err)
	}

	sessionSigner := manager.SessionSigner()
	if sessionSigner == nil {
		t.Error("Expected non-nil session signer")
	}

	// Test signing and verifying session ID
	testSessionID := "test-session-123"
	signedParam := sessionSigner.SignSessionID(testSessionID)
	if signedParam == "" {
		t.Error("Expected non-empty signed parameter")
	}

	// Verify the signed parameter
	verifiedSessionID, err := sessionSigner.VerifySessionID(signedParam)
	if err != nil {
		t.Errorf("Expected no error verifying session ID, got: %v", err)
	}

	if verifiedSessionID != testSessionID {
		t.Errorf("Expected verified session ID %s, got %s", testSessionID, verifiedSessionID)
	}
}
