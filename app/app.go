package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/app/metrics"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
	"github.com/zerodha/kite-mcp-server/mcp"
	"github.com/zerodha/kite-mcp-server/oauth"
	"github.com/zerodha/kite-mcp-server/web"
)

// App represents the main application structure
type App struct {
	Config           *Config
	Version          string
	startTime        time.Time
	kcManager        *kc.Manager
	oauthServer      *oauth.Server
	jwtOauthHandlers *oauth.Handlers
	logger           *slog.Logger
	metrics          *metrics.Manager
	rateLimiter      *web.RateLimiter
}

// Config holds the application configuration
type Config struct {
	KiteAPIKey              string
	KiteAPISecret           string
	AppPort                 string
	AppHost                 string
	ExcludedTools           string
	AdminSecretPath         string
	JWTSecret               string
	OAuthIssuer             string
	OAuthTokenTTL           string
	MCPSessionTimeout       string
	AllowedRedirectPatterns []string
	TrustedProxyCIDRs       []string
}

const (
	DefaultPort              = "8080"
	DefaultHost              = "localhost"
	DefaultOAuthTokenTTL     = "24h"
	DefaultMCPSessionTimeout = "24h"
)

func NewApp(logger *slog.Logger) *App {
	return &App{
		Config: &Config{
			KiteAPIKey:              os.Getenv("KITE_API_KEY"),
			KiteAPISecret:           os.Getenv("KITE_API_SECRET"),
			AppPort:                 os.Getenv("APP_PORT"),
			AppHost:                 os.Getenv("APP_HOST"),
			ExcludedTools:           os.Getenv("EXCLUDED_TOOLS"),
			AdminSecretPath:         os.Getenv("ADMIN_ENDPOINT_SECRET_PATH"),
			JWTSecret:               os.Getenv("JWT_SECRET"),
			OAuthIssuer:             os.Getenv("OAUTH_ISSUER"),
			OAuthTokenTTL:           os.Getenv("OAUTH_TOKEN_TTL"),
			MCPSessionTimeout:       os.Getenv("MCP_SESSION_TIMEOUT"),
			AllowedRedirectPatterns: parseCSVEnv(os.Getenv("ALLOWED_REDIRECT_PATTERNS")),
			TrustedProxyCIDRs:       parseCSVEnv(os.Getenv("TRUSTED_PROXY_CIDRS")),
		},
		Version:   "v0.0.0",
		startTime: time.Now(),
		logger:    logger,
	}
}

func parseCSVEnv(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func (app *App) SetVersion(version string) {
	app.Version = version
}

func (app *App) LoadConfig() error {
	if app.Config.AppPort == "" {
		app.Config.AppPort = DefaultPort
	}
	if app.Config.AppHost == "" {
		app.Config.AppHost = DefaultHost
	}
	if app.Config.KiteAPIKey == "" || app.Config.KiteAPISecret == "" {
		return fmt.Errorf("KITE_API_KEY or KITE_API_SECRET is missing")
	}
	if len(app.Config.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 bytes (got %d)", len(app.Config.JWTSecret))
	}
	// Default OAuth issuer to local URL if not set
	if app.Config.OAuthIssuer == "" {
		app.Config.OAuthIssuer = "http://" + app.Config.AppHost + ":" + app.Config.AppPort
	}
	if app.Config.OAuthTokenTTL == "" {
		app.Config.OAuthTokenTTL = DefaultOAuthTokenTTL
	}
	if _, err := time.ParseDuration(app.Config.OAuthTokenTTL); err != nil {
		return fmt.Errorf("invalid OAUTH_TOKEN_TTL: %w", err)
	}
	if tokenTTL, _ := time.ParseDuration(app.Config.OAuthTokenTTL); tokenTTL <= 0 {
		return fmt.Errorf("OAUTH_TOKEN_TTL must be greater than zero")
	} else if tokenTTL > kc.DefaultSessionDuration {
		return fmt.Errorf("OAUTH_TOKEN_TTL must not exceed the Kite session lifetime of %s", kc.DefaultSessionDuration)
	}
	if app.Config.MCPSessionTimeout == "" {
		app.Config.MCPSessionTimeout = DefaultMCPSessionTimeout
	}
	if _, err := time.ParseDuration(app.Config.MCPSessionTimeout); err != nil {
		return fmt.Errorf("invalid MCP_SESSION_TIMEOUT: %w", err)
	}
	if sessionTimeout, _ := time.ParseDuration(app.Config.MCPSessionTimeout); sessionTimeout <= 0 {
		return fmt.Errorf("MCP_SESSION_TIMEOUT must be greater than zero")
	}
	for _, cidr := range app.Config.TrustedProxyCIDRs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("invalid TRUSTED_PROXY_CIDRS entry %q: %w", cidr, err)
		}
	}
	return nil
}

func (app *App) RunServer() error {
	url := app.buildServerURL()
	app.configureHTTPClient()
	mcpServer, err := app.initializeServices()
	if err != nil {
		return err
	}
	srv := app.createHTTPServer(url)
	app.setupGracefulShutdown(srv)
	return app.startServer(srv, mcpServer, url)
}

func (app *App) buildServerURL() string {
	return app.Config.AppHost + ":" + app.Config.AppPort
}

func (app *App) configureHTTPClient() {
	http.DefaultClient.Timeout = 30 * time.Second
}

func (app *App) initializeServices() (*mcpsdk.Server, error) {
	app.logger.Info("Initializing services...")
	tokenTTL, err := time.ParseDuration(app.Config.OAuthTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("invalid OAUTH_TOKEN_TTL: %w", err)
	}
	// --- Instruments Manager ---
	instManager, err := instruments.New(instruments.Config{Logger: app.logger})
	if err != nil {
		return nil, fmt.Errorf("failed to create instruments manager: %w", err)
	}

	// --- Metrics & Rate Limiter ---
	app.metrics = metrics.New(metrics.Config{
		ServiceName:     "kite-mcp-server",
		AdminSecretPath: app.Config.AdminSecretPath,
		AutoCleanup:     true,
	})
	app.rateLimiter = web.NewRateLimiter(app.Config.TrustedProxyCIDRs...)

	// --- Kite Connect Manager ---
	kcManager, err := kc.New(kc.Config{
		APIKey:          app.Config.KiteAPIKey,
		APISecret:       app.Config.KiteAPISecret,
		Logger:          app.logger,
		Metrics:         app.metrics,
		Instruments:     instManager,
		SessionDuration: max(tokenTTL, kc.DefaultSessionDuration),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kite Connect manager: %w", err)
	}
	app.kcManager = kcManager

	// --- JWT OAuth Server ---
	app.oauthServer = oauth.New(oauth.Config{
		Issuer:                  app.Config.OAuthIssuer,
		JWTSecret:               []byte(app.Config.JWTSecret),
		TokenTTL:                tokenTTL,
		AllowedRedirectPatterns: app.Config.AllowedRedirectPatterns,
		TrustedProxyCIDRs:       app.Config.TrustedProxyCIDRs,
	})
	app.jwtOauthHandlers = oauth.NewHandlers(app.oauthServer, app.kcManager, app.logger)

	app.logger.Info("Static docs site ready (moat-generated)")

	// --- MCP Server & Tools ---
	app.logger.Info("Creating MCP server and registering tools...")
	mcpServer := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "Kite MCP Server", Version: app.Version},
		nil,
	)
	mcp.RegisterTools(mcpServer, kcManager, app.Config.ExcludedTools, app.logger)

	app.logger.Info("All services initialized.")
	return mcpServer, nil
}

func (app *App) createHTTPServer(url string) *http.Server {
	return &http.Server{Addr: url}
}

func (app *App) setupGracefulShutdown(srv *http.Server) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		defer stop()
		<-ctx.Done()
		app.logger.Info("Shutting down server...")

		// Shutdown services
		app.kcManager.Shutdown()
		app.metrics.Shutdown()

		// Shutdown HTTP server
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			app.logger.Error("Server shutdown error", "error", err)
		}
		app.logger.Info("Server shutdown complete")
	}()
}

func (app *App) setupMux() *http.ServeMux {
	mux := http.NewServeMux()
	if app.Config.AdminSecretPath != "" {
		mux.HandleFunc("/admin/", app.metrics.AdminHTTPHandler())
	}

	// Docs routes (moat-generated static site)
	mux.Handle("/", NewDocsHandler())

	// OAuth endpoints (JWT-based)
	mux.HandleFunc("/callback", app.jwtOauthHandlers.HandleCallback)
	mux.Handle("/authorize", app.rateLimiter.Middleware(http.HandlerFunc(app.jwtOauthHandlers.HandleAuthorize)))
	mux.Handle("/token", app.rateLimiter.Middleware(http.HandlerFunc(app.jwtOauthHandlers.HandleToken)))
	mux.HandleFunc("/.well-known/oauth-authorization-server", app.jwtOauthHandlers.HandleDiscovery)
	mux.HandleFunc("/.well-known/oauth-protected-resource", app.jwtOauthHandlers.HandleProtectedResourceMetadata)
	mux.HandleFunc("/register", app.jwtOauthHandlers.HandleRegister)
	return mux
}

func (app *App) serveHTTPServer(srv *http.Server) error {
	err := srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}
	return nil
}

// securityHeaders wraps a handler with standard security headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (app *App) startServer(srv *http.Server, mcpServer *mcpsdk.Server, url string) error {
	app.logger.Info("Starting MCP server", "url", "http://"+srv.Addr+"/mcp")
	sessionTimeout, err := time.ParseDuration(app.Config.MCPSessionTimeout)
	if err != nil {
		return fmt.Errorf("invalid MCP_SESSION_TIMEOUT: %w", err)
	}
	streamable := mcpsdk.NewStreamableHTTPHandler(
		func(r *http.Request) *mcpsdk.Server { return mcpServer },
		&mcpsdk.StreamableHTTPOptions{
			Logger:         app.logger,
			SessionTimeout: sessionTimeout,
		},
	)
	mux := app.setupMux()
	mux.Handle("/mcp", app.oauthServer.Middleware(http.HandlerFunc(streamable.ServeHTTP)))
	srv.Handler = securityHeaders(mux)
	return app.serveHTTPServer(srv)
}
