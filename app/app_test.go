package app

import (
	"io"
	"log/slog"
	"os"
	"testing"
)

// testLogger creates a discard logger for tests
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLoadConfig_MissingAPIKey(t *testing.T) {
	// Clear environment variables
	_ = os.Unsetenv("KITE_API_KEY")
	_ = os.Unsetenv("KITE_API_SECRET")

	app := NewApp(testLogger())
	err := app.LoadConfig()

	if err == nil {
		t.Error("Expected error when API key and secret are missing")
	}

	expectedMsg := "KITE_API_KEY or KITE_API_SECRET is missing"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestLoadConfig_MissingAPISecret(t *testing.T) {
	// Set only API key
	_ = os.Setenv("KITE_API_KEY", "test_key")
	_ = os.Unsetenv("KITE_API_SECRET")
	defer func() { _ = os.Unsetenv("KITE_API_KEY") }()

	app := NewApp(testLogger())
	err := app.LoadConfig()

	if err == nil {
		t.Error("Expected error when API secret is missing")
	}

	expectedMsg := "KITE_API_KEY or KITE_API_SECRET is missing"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestLoadConfig_MissingJWTSecret(t *testing.T) {
	// Set API key and secret but not JWT secret
	_ = os.Setenv("KITE_API_KEY", "test_key")
	_ = os.Setenv("KITE_API_SECRET", "test_secret")
	_ = os.Unsetenv("JWT_SECRET")
	defer func() {
		_ = os.Unsetenv("KITE_API_KEY")
		_ = os.Unsetenv("KITE_API_SECRET")
	}()

	app := NewApp(testLogger())
	err := app.LoadConfig()

	if err == nil {
		t.Error("Expected error when JWT secret is missing")
	}

	expectedMsg := "JWT_SECRET must be at least 32 bytes (got 0)"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestLoadConfig_ValidCredentials(t *testing.T) {
	// Set both API key, secret, and JWT secret
	_ = os.Setenv("KITE_API_KEY", "test_key")
	_ = os.Setenv("KITE_API_SECRET", "test_secret")
	_ = os.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimum")
	defer func() {
		_ = os.Unsetenv("KITE_API_KEY")
		_ = os.Unsetenv("KITE_API_SECRET")
		_ = os.Unsetenv("JWT_SECRET")
	}()

	app := NewApp(testLogger())
	err := app.LoadConfig()

	if err != nil {
		t.Errorf("Expected no error with valid credentials, got: %v", err)
	}

	// Verify config values
	if app.Config.KiteAPIKey != "test_key" {
		t.Errorf("Expected API key 'test_key', got '%s'", app.Config.KiteAPIKey)
	}

	if app.Config.KiteAPISecret != "test_secret" {
		t.Errorf("Expected API secret 'test_secret', got '%s'", app.Config.KiteAPISecret)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear all environment variables
	_ = os.Unsetenv("APP_MODE")
	_ = os.Unsetenv("APP_PORT")
	_ = os.Unsetenv("APP_HOST")
	t.Setenv("OAUTH_TOKEN_TTL", "")
	t.Setenv("MCP_SESSION_TIMEOUT", "")
	_ = os.Setenv("KITE_API_KEY", "test_key")
	_ = os.Setenv("KITE_API_SECRET", "test_secret")
	_ = os.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimum")
	defer func() {
		_ = os.Unsetenv("KITE_API_KEY")
		_ = os.Unsetenv("KITE_API_SECRET")
		_ = os.Unsetenv("JWT_SECRET")
	}()

	app := NewApp(testLogger())
	err := app.LoadConfig()

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Verify defaults
	if app.Config.AppPort != DefaultPort {
		t.Errorf("Expected default port '%s', got '%s'", DefaultPort, app.Config.AppPort)
	}

	if app.Config.AppHost != DefaultHost {
		t.Errorf("Expected default host '%s', got '%s'", DefaultHost, app.Config.AppHost)
	}

	// Verify OAuth issuer default
	expectedIssuer := "http://" + DefaultHost + ":" + DefaultPort
	if app.Config.OAuthIssuer != expectedIssuer {
		t.Errorf("Expected OAuth issuer '%s', got '%s'", expectedIssuer, app.Config.OAuthIssuer)
	}

	if app.Config.OAuthTokenTTL != DefaultOAuthTokenTTL {
		t.Errorf("Expected default OAuth token TTL %q, got %q", DefaultOAuthTokenTTL, app.Config.OAuthTokenTTL)
	}

	if app.Config.MCPSessionTimeout != DefaultMCPSessionTimeout {
		t.Errorf("Expected default MCP session timeout %q, got %q", DefaultMCPSessionTimeout, app.Config.MCPSessionTimeout)
	}
}

func TestLoadConfig_OAuthTokenTTL(t *testing.T) {
	t.Setenv("KITE_API_KEY", "test_key")
	t.Setenv("KITE_API_SECRET", "test_secret")
	t.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimum")
	t.Setenv("OAUTH_TOKEN_TTL", "12h")

	app := NewApp(testLogger())
	if err := app.LoadConfig(); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if app.Config.OAuthTokenTTL != "12h" {
		t.Errorf("Expected OAuth token TTL %q, got %q", "12h", app.Config.OAuthTokenTTL)
	}
}

func TestLoadConfig_InvalidOAuthTokenTTL(t *testing.T) {
	t.Setenv("KITE_API_KEY", "test_key")
	t.Setenv("KITE_API_SECRET", "test_secret")
	t.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimum")
	t.Setenv("OAUTH_TOKEN_TTL", "tomorrow")

	app := NewApp(testLogger())
	err := app.LoadConfig()
	if err == nil {
		t.Fatal("Expected error for invalid OAuth token TTL")
	}
	if got, want := err.Error(), "invalid OAUTH_TOKEN_TTL: time: invalid duration \"tomorrow\""; got != want {
		t.Errorf("Expected error %q, got %q", want, got)
	}
}

func TestLoadConfig_MCPSessionTimeout(t *testing.T) {
	t.Setenv("KITE_API_KEY", "test_key")
	t.Setenv("KITE_API_SECRET", "test_secret")
	t.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimum")
	t.Setenv("MCP_SESSION_TIMEOUT", "45m")

	app := NewApp(testLogger())
	if err := app.LoadConfig(); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if app.Config.MCPSessionTimeout != "45m" {
		t.Errorf("Expected MCP session timeout %q, got %q", "45m", app.Config.MCPSessionTimeout)
	}
}

func TestLoadConfig_InvalidMCPSessionTimeout(t *testing.T) {
	t.Setenv("KITE_API_KEY", "test_key")
	t.Setenv("KITE_API_SECRET", "test_secret")
	t.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimum")
	t.Setenv("MCP_SESSION_TIMEOUT", "tomorrow")

	app := NewApp(testLogger())
	err := app.LoadConfig()
	if err == nil {
		t.Fatal("Expected error for invalid MCP session timeout")
	}
	if got, want := err.Error(), "invalid MCP_SESSION_TIMEOUT: time: invalid duration \"tomorrow\""; got != want {
		t.Errorf("Expected error %q, got %q", want, got)
	}
}

func TestLoadConfig_AllowedRedirectPatterns(t *testing.T) {
	_ = os.Setenv("KITE_API_KEY", "test_key")
	_ = os.Setenv("KITE_API_SECRET", "test_secret")
	_ = os.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimum")
	_ = os.Setenv("ALLOWED_REDIRECT_PATTERNS", "localhost, https://claude.ai/api/mcp/auth_callback, https://chatgpt.com/connector_platform_oauth_redirect ")
	defer func() {
		_ = os.Unsetenv("KITE_API_KEY")
		_ = os.Unsetenv("KITE_API_SECRET")
		_ = os.Unsetenv("JWT_SECRET")
		_ = os.Unsetenv("ALLOWED_REDIRECT_PATTERNS")
	}()

	app := NewApp(testLogger())
	if err := app.LoadConfig(); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expected := []string{
		"localhost",
		"https://claude.ai/api/mcp/auth_callback",
		"https://chatgpt.com/connector_platform_oauth_redirect",
	}
	if len(app.Config.AllowedRedirectPatterns) != len(expected) {
		t.Fatalf("Expected %d patterns, got %d", len(expected), len(app.Config.AllowedRedirectPatterns))
	}
	for i := range expected {
		if app.Config.AllowedRedirectPatterns[i] != expected[i] {
			t.Fatalf("Expected pattern %q at index %d, got %q", expected[i], i, app.Config.AllowedRedirectPatterns[i])
		}
	}
}

func TestNewApp(t *testing.T) {
	app := NewApp(testLogger())

	if app == nil {
		t.Error("Expected non-nil app")
		return
	}

	if app.Config == nil {
		t.Error("Expected non-nil config")
	}

	if app.Version != "v0.0.0" {
		t.Errorf("Expected default version 'v0.0.0', got '%s'", app.Version)
	}
}

func TestSetVersion(t *testing.T) {
	app := NewApp(testLogger())
	testVersion := "v1.2.3"

	app.SetVersion(testVersion)

	if app.Version != testVersion {
		t.Errorf("Expected version '%s', got '%s'", testVersion, app.Version)
	}
}
