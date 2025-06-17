package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/ory/fosite"
	"github.com/zerodha/kite-mcp-server/app/metrics"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/templates"
	"github.com/zerodha/kite-mcp-server/mcp"
	"github.com/zerodha/kite-mcp-server/oauth"
	"golang.org/x/time/rate"
)

// App represents the main application structure
type App struct {
	Config         *Config
	Version        string
	startTime      time.Time
	kcManager      *kc.Manager
	fositeProvider fosite.OAuth2Provider
	fositeStore    *oauth.InMemoryStore // Keep a reference to the concrete store for client creation
	statusTemplate *template.Template
	logger         *slog.Logger
	metrics        *metrics.Manager
	// For rate limiting
	rateLimiters   map[string]*rate.Limiter
	rateLimitersMu sync.Mutex
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
		Version:      "v0.0.0",
		startTime:    time.Now(),
		logger:       logger,
		rateLimiters: make(map[string]*rate.Limiter),
		metrics: metrics.New(metrics.Config{
			ServiceName:     "kite-mcp-server",
			AdminSecretPath: os.Getenv("ADMIN_ENDPOINT_SECRET_PATH"),
			AutoCleanup:     true,
		}),
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
	kcManager, mcpServer, err := app.initializeServices()
	if err != nil {
		return err
	}
	srv := app.createHTTPServer(url)
	app.setupGracefulShutdown(srv, kcManager)
	return app.startServer(srv, kcManager, mcpServer, url)
}

func (app *App) buildServerURL() string {
	return app.Config.AppHost + ":" + app.Config.AppPort
}

func (app *App) configureHTTPClient() {
	http.DefaultClient.Timeout = 30 * time.Second
}

func (app *App) initializeServices() (*kc.Manager, *server.MCPServer, error) {
	app.logger.Info("Initializing Fosite OAuth2 provider...")
	fositeConfig := &fosite.Config{
		AccessTokenLifespan:             time.Hour * 24,
		AuthorizeEndpointHandlers:       fosite.AuthorizeEndpointHandlers{},
		TokenEndpointHandlers:           fosite.TokenEndpointHandlers{},
		TokenIntrospectionHandlers:      fosite.TokenIntrospectionHandlers{},
		RevocationHandlers:              fosite.RevocationHandlers{},
		PushedAuthorizeEndpointHandlers: fosite.PushedAuthorizeEndpointHandlers{},
	}
	app.fositeStore = oauth.NewInMemoryStore()
	app.fositeProvider = oauth.NewFositeProvider(app.fositeStore, fositeConfig)
	app.logger.Info("Fosite OAuth2 provider initialized")

	app.logger.Info("Creating Kite Connect manager...")
	kcManager, err := kc.New(kc.Config{
		APIKey:    app.Config.KiteAPIKey,
		APISecret: app.Config.KiteAPISecret,
		Logger:    app.logger,
		Metrics:   app.metrics,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Kite Connect manager: %w", err)
	}
	app.kcManager = kcManager

	if err := app.initStatusPageTemplate(); err != nil {
		app.logger.Warn("Failed to initialize status template", "error", err)
	}

	app.logger.Info("Creating MCP server...")
	mcpServer := server.NewMCPServer("Kite MCP Server", app.Version)
	mcp.RegisterTools(mcpServer, kcManager, app.Config.ExcludedTools, app.logger)

	return kcManager, mcpServer, nil
}

func (app *App) createHTTPServer(url string) *http.Server {
	return &http.Server{Addr: url}
}

func (app *App) setupGracefulShutdown(srv *http.Server, kcManager *kc.Manager) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		defer stop()
		<-ctx.Done()
		app.logger.Info("Shutting down server...")
		kcManager.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			app.logger.Error("Server shutdown error", "error", err)
		}
		app.logger.Info("Server shutdown complete")
	}()
}

func (app *App) startServer(srv *http.Server, kcManager *kc.Manager, mcpServer *server.MCPServer, url string) error {
	switch app.Config.AppMode {
	default:
		return fmt.Errorf("invalid APP_MODE: %s", app.Config.AppMode)
	case ModeHybrid, ModeHTTP, ModeSSE:
		app.startHybridServer(srv, kcManager, mcpServer, url)
	case ModeStdIO:
		app.startStdIOServer(srv, kcManager, mcpServer)
	}
	return nil
}

func (app *App) setupMux(kcManager *kc.Manager) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", app.handleKiteCallback)
	if app.Config.AdminSecretPath != "" {
		mux.HandleFunc("/admin/", app.metrics.AdminHTTPHandler())
	}
	app.registerOAuthEndpoints(mux)
	app.serveStatusPage(mux)
	return mux
}

func (app *App) serveHTTPServer(srv *http.Server) {
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		app.logger.Error("HTTP server error", "error", err)
	}
}

func (app *App) startHybridServer(srv *http.Server, kcManager *kc.Manager, mcpServer *server.MCPServer, url string) {
	app.logger.Info("Starting Hybrid MCP server", "url", "http://"+url)
	sse := server.NewSSEServer(mcpServer, server.WithBaseURL(url), server.WithKeepAlive(true))
	streamable := server.NewStreamableHTTPServer(mcpServer, server.WithSessionIdManager(kcManager.SessionManager()))
	mux := app.setupMux(kcManager)
	mux.HandleFunc("/sse", sse.ServeHTTP)
	mux.HandleFunc("/message", sse.ServeHTTP)
	mux.Handle("/mcp", app.oauthMiddleware(http.HandlerFunc(streamable.ServeHTTP)))
	srv.Handler = mux
	app.serveHTTPServer(srv)
}

func (app *App) startStdIOServer(srv *http.Server, kcManager *kc.Manager, mcpServer *server.MCPServer) {
	app.logger.Info("Starting STDIO MCP server...")
	stdio := server.NewStdioServer(mcpServer)
	mux := app.setupMux(kcManager)
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

func (app *App) serveStatusPage(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
	})
}

// mustGenerateRandomString generates a random string of a given length or panics.
func mustGenerateRandomString(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

// --- Rate Limiting and OAuth 2.1 Handlers ---

func (app *App) getRateLimiter(ip string) *rate.Limiter {
	app.rateLimitersMu.Lock()
	defer app.rateLimitersMu.Unlock()
	limiter, exists := app.rateLimiters[ip]
	if !exists {
		limiter = rate.NewLimiter(rate.Every(12*time.Second), 5)
		app.rateLimiters[ip] = limiter
	}
	return limiter
}

func (app *App) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !app.getRateLimiter(ip).Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (app *App) registerOAuthEndpoints(mux *http.ServeMux) {
	mux.Handle("/authorize", app.rateLimitMiddleware(http.HandlerFunc(app.authorizeHandler)))
	mux.Handle("/token", app.rateLimitMiddleware(http.HandlerFunc(app.tokenHandler)))
	mux.Handle("/register", app.rateLimitMiddleware(http.HandlerFunc(app.registrationHandler)))
	mux.HandleFunc("/.well-known/oauth-authorization-server", app.discoveryHandler)
}

func (app *App) authorizeHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ar, err := app.fositeProvider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		app.fositeProvider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}
	kiteSessionID := app.kcManager.GenerateSession()
	kiteLoginURL, err := app.kcManager.SessionLoginURL(kiteSessionID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := app.kcManager.SessionManager().UpdateSessionData(kiteSessionID, ar); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, kiteLoginURL, http.StatusFound)
}

func (app *App) tokenHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mySessionData := &fosite.DefaultSession{}
	accessRequest, err := app.fositeProvider.NewAccessRequest(ctx, r, mySessionData)
	if err != nil {
		app.fositeProvider.WriteAccessError(ctx, w, accessRequest, err)
		return
	}
	accessResponse, err := app.fositeProvider.NewAccessResponse(ctx, accessRequest)
	if err != nil {
		app.fositeProvider.WriteAccessError(ctx, w, accessRequest, err)
		return
	}
	app.fositeProvider.WriteAccessResponse(ctx, w, accessRequest, accessResponse)
}

func (app *App) handleKiteCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m := app.kcManager

	q := r.URL.Query()
	requestToken := q.Get("request_token")
	signedSessionID := q.Get("session_id")

	mcpSessionID, err := m.SessionSigner().VerifySessionID(signedSessionID)
	if err != nil {
		http.Error(w, "Invalid callback parameters", http.StatusBadRequest)
		return
	}
	sessionData, err := m.SessionManager().GetSessionData(mcpSessionID)
	if err != nil {
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	if ar, ok := sessionData.(fosite.AuthorizeRequester); ok {
		if err := m.CompleteSession(mcpSessionID, requestToken); err != nil {
			app.fositeProvider.WriteAuthorizeError(ctx, w, ar, err)
			return
		}
		kiteSession, _ := m.GetSession(mcpSessionID)
		profile, err := kiteSession.Kite.Client.GetUserProfile()
		if err != nil {
			app.fositeProvider.WriteAuthorizeError(ctx, w, ar, err)
			return
		}
		mySession := &fosite.DefaultSession{Subject: profile.UserID}
		for _, scope := range ar.GetGrantedScopes() {
			ar.GrantScope(scope)
		}
		response, err := app.fositeProvider.NewAuthorizeResponse(ctx, ar, mySession)
		if err != nil {
			app.fositeProvider.WriteAuthorizeError(ctx, w, ar, err)
			return
		}
		app.fositeProvider.WriteAuthorizeResponse(ctx, w, ar, response)
		m.ClearSession(mcpSessionID)
		return
	}
	if err := m.CompleteSession(mcpSessionID, requestToken); err != nil {
		http.Error(w, "Failed to complete Kite session", http.StatusInternalServerError)
		return
	}
	if err := app.kcManager.RenderSuccessTemplate(w); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (app *App) registrationHandler(w http.ResponseWriter, r *http.Request) {
	client := &fosite.DefaultClient{
		GrantTypes:    fosite.Arguments{"authorization_code", "refresh_token", "client_credentials"},
		ResponseTypes: fosite.Arguments{"code", "id_token"},
		Scopes:        fosite.Arguments{"openid", "offline"},
	}

	decoder := json.NewDecoder(r.Body)
	var registrationRequest struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		GrantTypes   []string `json:"grant_types"`
	}
	if err := decoder.Decode(&registrationRequest); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	client.ID = "client-" + mustGenerateRandomString(12)
	client.RedirectURIs = registrationRequest.RedirectURIs
	if len(registrationRequest.GrantTypes) > 0 {
		client.GrantTypes = registrationRequest.GrantTypes
	}

	secret := "secret-" + mustGenerateRandomString(24)
	hashedSecret, err := oauth.HashSecret(secret)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	client.Secret = hashedSecret

	app.fositeStore.AddClient(client)
	app.logger.Info("Successfully registered new dynamic client", "client_id", client.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"client_id":                client.GetID(),
		"client_secret":            secret,
		"grant_types":              client.GetGrantTypes(),
		"redirect_uris":            client.GetRedirectURIs(),
		"client_name":              registrationRequest.ClientName,
		"client_id_issued_at":      time.Now().Unix(),
		"client_secret_expires_at": 0,
	})
}

func (app *App) discoveryHandler(w http.ResponseWriter, r *http.Request) {
	issuer := "http://" + r.Host // Should be https in production
	response := map[string]interface{}{
		"issuer":                 issuer,
		"authorization_endpoint": issuer + "/authorize",
		"token_endpoint":         issuer + "/token",
		"jwks_uri":               issuer + "/.well-known/jwks.json", // Placeholder
		"registration_endpoint":  issuer + "/register",
		"scopes_supported":       []string{"openid", "offline"},
		"response_types_supported": []string{
			"code",
			"id_token",
			"token",
			"code id_token",
			"code token",
			"id_token token",
			"code id_token token",
		},
		"grant_types_supported":                 []string{"authorization_code", "client_credentials", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (app *App) oauthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token := fosite.AccessTokenFromRequest(r)
		if token == "" {
			// Send 401 Unauthorized if no token is provided.
			http.Error(w, "Unauthorized: No access token provided", http.StatusUnauthorized)
			return
		}

		_, ar, err := app.fositeProvider.IntrospectToken(ctx, token, fosite.AccessToken, &fosite.DefaultSession{})
		if err != nil {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}
		// Set the authenticated session ID in the header for the next handler.
		r.Header.Set("Mcp-Session-Id", ar.GetSession().GetSubject())

		// If no token, the next handler will check for Mcp-Session-Id or fail.
		// This allows both OAuth and simple header-based auth to coexist.
		next.ServeHTTP(w, r)
	})
}
