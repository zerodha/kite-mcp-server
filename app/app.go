package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/ory/fosite"
	"github.com/zerodha/kite-mcp-server/app/metrics"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
	"github.com/zerodha/kite-mcp-server/kc/templates"
	"github.com/zerodha/kite-mcp-server/mcp"
	"github.com/zerodha/kite-mcp-server/oauth"
	"github.com/zerodha/kite-mcp-server/web"
)

// App represents the main application structure
type App struct {
	Config         *Config
	Version        string
	startTime      time.Time
	kcManager      *kc.Manager
	fositeProvider fosite.OAuth2Provider
	fositeStore    *oauth.InMemoryStore
	statusTemplate *template.Template
	logger         *slog.Logger
	metrics        *metrics.Manager
	rateLimiter    *web.RateLimiter
	oauthHandler   *web.OAuthHandler
}

// StatusPageData holds template data for the status page
type StatusPageData struct {
	Title   string
	Version string
	Mode    string
}

// Config holds the application configuration
type Config struct {
	KiteAPIKey      string
	KiteAPISecret   string
	AppMode         string
	AppPort         string
	AppHost         string
	ExcludedTools   string
	AdminSecretPath string
}

// Server mode constants
const (
	ModeSSE    = "sse"
	ModeStdIO  = "stdio"
	ModeHTTP   = "http"
	ModeHybrid = "hybrid"

	DefaultPort    = "8080"
	DefaultHost    = "localhost"
	DefaultAppMode = "http"
)

func NewApp(logger *slog.Logger) *App {
	return &App{
		Config: &Config{
			KiteAPIKey:      os.Getenv("KITE_API_KEY"),
			KiteAPISecret:   os.Getenv("KITE_API_SECRET"),
			AppMode:         os.Getenv("APP_MODE"),
			AppPort:         os.Getenv("APP_PORT"),
			AppHost:         os.Getenv("APP_HOST"),
			ExcludedTools:   os.Getenv("EXCLUDED_TOOLS"),
			AdminSecretPath: os.Getenv("ADMIN_ENDPOINT_SECRET_PATH"),
		},
		Version:   "v0.0.0",
		startTime: time.Now(),
		logger:    logger,
	}
}

func (app *App) SetVersion(version string) {
	app.Version = version
}

func (app *App) LoadConfig() error {
	if app.Config.AppMode == "" {
		app.Config.AppMode = DefaultAppMode
	}
	if app.Config.AppPort == "" {
		app.Config.AppPort = DefaultPort
	}
	if app.Config.AppHost == "" {
		app.Config.AppHost = DefaultHost
	}
	if app.Config.KiteAPIKey == "" || app.Config.KiteAPISecret == "" {
		return fmt.Errorf("KITE_API_KEY or KITE_API_SECRET is missing")
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

func (app *App) initializeServices() (*server.MCPServer, error) {
	app.logger.Info("Initializing services...")
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
	app.rateLimiter = web.NewRateLimiter()

	// --- Fosite OAuth2 Provider ---
	// The HMAC strategy used by Fosite for signing tokens requires a secret key.
	// The error "secret for signing HMAC-SHA512/256 is expected to be 32 byte long, got 0 byte"
	// indicates this was not set. We'll use the Kite API Secret as a source of entropy
	// and hash it with SHA-256 to produce a 32-byte key.
	key := sha256.Sum256([]byte(app.Config.KiteAPISecret))
	fositeConfig := &fosite.Config{
		GlobalSecret:        key[:],
		AccessTokenLifespan: time.Hour * 24,
	}
	app.fositeStore = oauth.NewInMemoryStore()
	app.fositeProvider = oauth.NewFositeProvider(app.fositeStore, fositeConfig)

	// --- Kite Connect Manager ---
	kcManager, err := kc.New(kc.Config{
		APIKey:      app.Config.KiteAPIKey,
		APISecret:   app.Config.KiteAPISecret,
		Logger:      app.logger,
		Metrics:     app.metrics,
		Instruments: instManager,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kite Connect manager: %w", err)
	}
	app.kcManager = kcManager

	// --- OAuth Handler ---
	oauthHandlerConfig := web.AppConfig{
		Host:       app.Config.AppHost + ":" + app.Config.AppPort,
		KiteAPIKey: app.Config.KiteAPIKey,
	}
	app.oauthHandler = web.NewOAuthHandler(app.fositeProvider, app.fositeStore, app.kcManager, app.logger, oauthHandlerConfig)

	if err := app.initStatusPageTemplate(); err != nil {
		app.logger.Warn("Failed to initialize status template", "error", err)
	}

	// --- MCP Server & Tools ---
	app.logger.Info("Creating MCP server and registering tools...")
	mcpServer := server.NewMCPServer("Kite MCP Server", app.Version)
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
	mux.HandleFunc("/", app.serveStatusPage)
	mux.HandleFunc("/callback", app.oauthHandler.Callback)
	mux.Handle("/authorize", app.rateLimiter.Middleware(http.HandlerFunc(app.oauthHandler.Authorize)))
	mux.Handle("/token", app.rateLimiter.Middleware(http.HandlerFunc(app.oauthHandler.Token)))
	mux.Handle("/register", app.rateLimiter.Middleware(http.HandlerFunc(app.oauthHandler.Register)))
	mux.HandleFunc("/.well-known/oauth-authorization-server", app.oauthHandler.Discovery)
	mux.HandleFunc("/.well-known/oauth-protected-resource", app.oauthHandler.ProtectedResourceMetadata)
	return mux
}

func (app *App) serveHTTPServer(srv *http.Server) {
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		app.logger.Error("HTTP server error", "error", err)
	}
}

func (app *App) startServer(srv *http.Server, mcpServer *server.MCPServer, url string) error {
	switch app.Config.AppMode {
	default:
		return fmt.Errorf("invalid APP_MODE: %s", app.Config.AppMode)
	case ModeHybrid:
		app.startHybridServerMode(srv, mcpServer, url)
	case ModeHTTP:
		app.startHTTPServerMode(srv, mcpServer)
	case ModeSSE:
		app.startSSEServerMode(srv, mcpServer, url)
	case ModeStdIO:
		app.startStdIOServer(srv, mcpServer)
	}
	return nil
}

func (app *App) startHTTPServerMode(srv *http.Server, mcpServer *server.MCPServer) {
	app.logger.Info("Starting HTTP MCP server", "url", "http://"+srv.Addr+"/mcp")
	streamable := server.NewStreamableHTTPServer(mcpServer, server.WithSessionIdManager(app.kcManager.SessionManager()))
	mux := app.setupMux()
	mux.Handle("/mcp", app.oauthHandler.Middleware(http.HandlerFunc(streamable.ServeHTTP)))
	srv.Handler = mux
	app.serveHTTPServer(srv)
}

func (app *App) startSSEServerMode(srv *http.Server, mcpServer *server.MCPServer, url string) {
	app.logger.Info("Starting SSE MCP server", "url", "http://"+url+"/sse")
	sse := server.NewSSEServer(mcpServer, server.WithBaseURL(url), server.WithKeepAlive(true))
	mux := app.setupMux()
	mux.HandleFunc("/sse", sse.ServeHTTP)
	mux.HandleFunc("/message", sse.ServeHTTP)
	srv.Handler = mux
	app.serveHTTPServer(srv)
}

func (app *App) startHybridServerMode(srv *http.Server, mcpServer *server.MCPServer, url string) {
	app.logger.Info("Starting Hybrid MCP server", "url", "http://"+url)
	sse := server.NewSSEServer(mcpServer, server.WithBaseURL(url), server.WithKeepAlive(true))
	streamable := server.NewStreamableHTTPServer(mcpServer, server.WithSessionIdManager(app.kcManager.SessionManager()))
	mux := app.setupMux()

	mux.HandleFunc("/sse", sse.ServeHTTP)
	mux.HandleFunc("/message", sse.ServeHTTP)
	mux.Handle("/mcp", app.oauthHandler.Middleware(http.HandlerFunc(streamable.ServeHTTP)))

	srv.Handler = mux
	app.serveHTTPServer(srv)
}

func (app *App) startStdIOServer(srv *http.Server, mcpServer *server.MCPServer) {
	app.logger.Info("Starting STDIO MCP server...")
	stdio := server.NewStdioServer(mcpServer)
	mux := app.setupMux()
	srv.Handler = mux
	go app.serveHTTPServer(srv)
	if err := stdio.Listen(context.Background(), os.Stdin, os.Stdout); err != nil {
		app.logger.Error("STDIO server error", "error", err)
	}
}

func (app *App) initStatusPageTemplate() error {
	tmpl, err := template.ParseFS(templates.FS, "base.html", "status.html")
	if err != nil {
		return fmt.Errorf("failed to parse status template: %w", err)
	}
	app.statusTemplate = tmpl
	return nil
}

func (app *App) getStatusData() StatusPageData {
	return StatusPageData{Title: "Status", Version: app.Version, Mode: app.Config.AppMode}
}

func (app *App) serveStatusPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if app.statusTemplate == nil {
		http.Error(w, "Status template not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := app.statusTemplate.ExecuteTemplate(w, "base", app.getStatusData()); err != nil {
		app.logger.Error("Failed to execute status template", "error", err)
	}
}
