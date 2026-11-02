package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

func TestLoadConfig_OAuthTokenTTLCannotExceedGrant(t *testing.T) {
	t.Setenv("KITE_API_KEY", "test_key")
	t.Setenv("KITE_API_SECRET", "test_secret")
	t.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimum")
	t.Setenv("OAUTH_TOKEN_TTL", "169h")

	err := NewApp(testLogger()).LoadConfig()
	if err == nil {
		t.Fatal("expected an error for a token longer than the seven-day grant")
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

func TestServeHTTPServerReturnsListenError(t *testing.T) {
	app := NewApp(testLogger())
	err := app.serveHTTPServer(&http.Server{Addr: "127.0.0.1:invalid-port"})
	if err == nil {
		t.Fatal("expected listen error")
	}
}

func TestLoadConfigAcceptsSevenDayToken(t *testing.T) {
	t.Setenv("KITE_API_KEY", "test_key")
	t.Setenv("KITE_API_SECRET", "test_secret")
	t.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimum")
	t.Setenv("OAUTH_TOKEN_TTL", "168h")
	if err := NewApp(testLogger()).LoadConfig(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigIssuerOrigin(t *testing.T) {
	for _, tc := range []struct {
		issuer string
		valid  bool
	}{
		{"https://mcp.example", true},
		{"https://mcp.example/", true},
		{"HTTPS://mcp.example", true},
		{"http://localhost:8080", true},
		{"http://[::1]:8080", true},
		{"https://user:password@mcp.example", false},
		{"https://mcp.example/prefix", false},
		{"https://mcp.example?", false},
		{"https://mcp.example?token=secret", false},
		{"https://mcp.example#", false},
		{"https://mcp.example#fragment", false},
		{"https://mcp.example:", false},
		{"https://mcp.example:99999", false},
		{"file:///tmp/server", false},
		{"mcp.example", false},
	} {
		t.Run(tc.issuer, func(t *testing.T) {
			app := NewApp(testLogger())
			app.Config = &Config{KiteAPIKey: "key", KiteAPISecret: "secret", JWTSecret: strings.Repeat("s", 32), OAuthIssuer: tc.issuer}
			if err := app.LoadConfig(); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if tc.valid && strings.HasSuffix(app.Config.OAuthIssuer, "/") {
				t.Fatal("issuer kept a trailing slash, which produces incorrect endpoint URLs")
			}
		})
	}
}

func TestDefaultIssuerSupportsIPv6(t *testing.T) {
	app := NewApp(testLogger())
	app.Config = &Config{KiteAPIKey: "key", KiteAPISecret: "secret", JWTSecret: strings.Repeat("s", 32), AppHost: "::1", AppPort: "8080"}
	if err := app.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	if got := app.buildServerURL(); got != "[::1]:8080" {
		t.Fatalf("invalid listener address: %q", got)
	}
	if got := app.Config.OAuthIssuer; got != "http://[::1]:8080" {
		t.Fatalf("invalid issuer: %q", got)
	}
}

func TestShutdownWaitsForActiveRequests(t *testing.T) {
	app := NewApp(testLogger())
	started, release, draining := make(chan struct{}), make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	srv.Config.RegisterOnShutdown(func() { close(draining) })
	stop := app.setupGracefulShutdown(srv.Config)
	defer stop()
	response := make(chan error, 1)
	go func() {
		r, err := srv.Client().Get(srv.URL)
		if r != nil {
			_ = r.Body.Close()
		}
		response <- err
	}()
	<-started
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	<-draining
	select {
	case <-done:
		t.Error("shutdown returned while an active request still needed services")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown failed to finish after requests drained")
	}
	if err := <-response; err != nil {
		t.Fatal(err)
	}
}
