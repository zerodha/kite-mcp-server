package oauth

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/zerodha/kite-mcp-server/kc"
)

// Handlers wraps the OAuth server with HTTP handlers
type Handlers struct {
	server    *Server
	kcManager *kc.Manager
	logger    *slog.Logger
}

// NewHandlers creates OAuth HTTP handlers
func NewHandlers(server *Server, kcManager *kc.Manager, logger *slog.Logger) *Handlers {
	return &Handlers{
		server:    server,
		kcManager: kcManager,
		logger:    logger,
	}
}

// pendingAuth stores the OAuth request while user authenticates with Kite
type pendingAuth struct {
	ClientID      string
	RedirectURI   string
	State         string
	CodeChallenge string
	Scope         string
}

// HandleAuthorize handles GET /authorize
func (h *Handlers) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	// Parse the OAuth request
	req, err := ParseAuthorizeRequest(r)
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Validate redirect URI against allowlist before auto-registering
	if err := h.server.ValidateRedirectURI(req.RedirectURI); err != nil {
		h.logger.Warn("rejected redirect_uri", "redirect_uri", req.RedirectURI, "error", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Auto-register client
	h.server.GetOrCreateClient(req.ClientID, req.RedirectURI)

	// Store OAuth request in session for callback
	pending := &pendingAuth{
		ClientID:      req.ClientID,
		RedirectURI:   req.RedirectURI,
		State:         req.State,
		CodeChallenge: req.CodeChallenge,
		Scope:         req.Scope,
	}

	// Generate a temporary session to track this OAuth flow
	tempSessionID := h.kcManager.SessionManager().GenerateWithData(pending)

	// Redirect to KiteConnect OAuth
	kiteLoginURL, err := h.kcManager.GenerateLoginURL(tempSessionID)
	if err != nil {
		h.logger.Error("failed to generate Kite login URL", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("redirecting to Kite OAuth", "client_id", req.ClientID)
	http.Redirect(w, r, kiteLoginURL, http.StatusFound)
}

// callbackRateLimiter limits /callback requests (30 per IP per minute)
var callbackRateLimiter = NewRateLimiter(30, time.Minute)

// HandleCallback handles GET /callback from KiteConnect
func (h *Handlers) HandleCallback(w http.ResponseWriter, r *http.Request) {
	clientIP := extractClientIP(r)
	if !callbackRateLimiter.Allow(clientIP) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	q := r.URL.Query()
	requestToken := q.Get("request_token")
	signedSessionID := q.Get("session_id")

	// Verify the signed session ID
	sessionID, err := h.kcManager.SessionSigner().VerifySessionID(signedSessionID)
	if err != nil {
		h.logger.Warn("invalid callback session", "error", err)
		http.Error(w, "authorization failed", http.StatusBadRequest)
		return
	}

	// Get the session
	session, err := h.kcManager.SessionManager().Get(sessionID)
	if err != nil {
		h.logger.Warn("callback session not found", "session_id", sessionID, "error", err)
		http.Error(w, "authorization failed", http.StatusBadRequest)
		return
	}

	// Complete KiteConnect login
	creds, err := h.kcManager.CompleteLogin(requestToken)
	if err != nil {
		h.logger.Error("failed to complete Kite login", "error", err)
		http.Error(w, "authorization failed", http.StatusInternalServerError)
		return
	}

	// Check if this is an OAuth flow or regular login tool flow
	pending, isOAuth := session.OAuthData.(*pendingAuth)
	if !isOAuth {
		// Regular login tool flow - store credentials and render success page
		if err := h.kcManager.SessionManager().UpdateCredentials(sessionID, creds); err != nil {
			h.logger.Error("failed to update session credentials", "error", err)
			http.Error(w, "session error", http.StatusInternalServerError)
			return
		}
		h.logger.Info("login tool callback", "user_id", creds.UserID)
		_ = h.kcManager.RenderSuccessTemplate(w)
		return
	}

	// OAuth flow - clean up temp session, create permanent user session
	defer func() { _, _ = h.kcManager.SessionManager().Terminate(sessionID) }()

	// Create or get persistent session for the user
	userSession, _, err := h.kcManager.SessionManager().GetOrCreate(creds.UserID)
	if err != nil {
		h.logger.Error("failed to create user session", "error", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	// Update credentials in the user session
	if err := h.kcManager.SessionManager().UpdateCredentials(userSession.ID, creds); err != nil {
		h.logger.Error("failed to update user session credentials", "error", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	// Generate authorization code
	code, err := h.server.CreateAuthCode(
		pending.ClientID,
		pending.RedirectURI,
		creds.UserID,
		userSession.ID,
		pending.CodeChallenge,
	)
	if err != nil {
		h.logger.Error("failed to create auth code", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Redirect back to client with code
	redirectURL := BuildRedirectWithCode(pending.RedirectURI, code, pending.State)
	h.logger.Info("OAuth callback complete", "client_id", pending.ClientID, "user_id", creds.UserID)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// HandleToken handles POST /token
func (h *Handlers) HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := ParseTokenRequest(r)
	if err != nil {
		h.logger.Warn("invalid token request", "error", err)
		WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_request",
			"error_description": "The token request is invalid.",
		})
		return
	}

	accessToken, err := h.server.ExchangeCode(req)
	if err != nil {
		h.logger.Warn("token exchange failed", "client_id", req.ClientID, "error", err)
		WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_grant",
			"error_description": "The authorization grant is invalid or expired.",
		})
		return
	}

	h.logger.Info("token issued", "client_id", req.ClientID)
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(h.server.config.TokenTTL.Seconds()),
		"scope":        "default",
	})
}

// HandleDiscovery handles GET /.well-known/oauth-authorization-server
func (h *Handlers) HandleDiscovery(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, h.server.AuthorizationServerMetadata())
}

// HandleProtectedResourceMetadata handles GET /.well-known/oauth-protected-resource
func (h *Handlers) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers per RFC 9728
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	WriteJSON(w, http.StatusOK, h.server.ProtectedResourceMetadata())
}

// registerRateLimiter limits /register requests (10 per IP per hour)
var registerRateLimiter = NewRateLimiter(10, time.Hour)

// RegisterRequest represents RFC 7591 client registration request
type RegisterRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
	ClientName   string   `json:"client_name,omitempty"`
}

// extractClientIP returns the client IP, stripping port from RemoteAddr.
// Does NOT trust X-Forwarded-For (should be handled at reverse proxy level).
func extractClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// HandleRegister handles POST /register (RFC 7591 Dynamic Client Registration)
func (h *Handlers) HandleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := extractClientIP(r)
	if !registerRateLimiter.Allow(clientIP) {
		WriteJSON(w, http.StatusTooManyRequests, map[string]string{
			"error":             "rate_limit_exceeded",
			"error_description": "Too many registration requests. Try again later.",
		})
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_client_metadata",
			"error_description": "Invalid request body.",
		})
		return
	}

	if len(req.RedirectURIs) == 0 {
		WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_client_metadata",
			"error_description": "redirect_uris is required",
		})
		return
	}

	// Validate all redirect URIs against allowlist
	for _, uri := range req.RedirectURIs {
		if err := h.server.ValidateRedirectURI(uri); err != nil {
			h.logger.Warn("rejected DCR redirect_uri", "redirect_uri", uri, "error", err)
			WriteJSON(w, http.StatusBadRequest, map[string]string{
				"error":             "invalid_redirect_uri",
				"error_description": "The redirect URI is not allowed for this MCP server.",
			})
			return
		}
	}

	clientID := generateSecureToken(16)

	h.server.mu.Lock()
	h.server.clients[clientID] = &Client{
		ID:           clientID,
		RedirectURIs: req.RedirectURIs,
		CreatedAt:    time.Now(),
	}
	h.server.mu.Unlock()

	h.logger.Info("client registered via DCR", "client_id", clientID, "name", req.ClientName)

	WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"redirect_uris":              req.RedirectURIs,
		"client_name":                req.ClientName,
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// RegisterRoutes registers all OAuth routes on a mux
func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-authorization-server", h.HandleDiscovery)
	mux.HandleFunc("/.well-known/oauth-protected-resource", h.HandleProtectedResourceMetadata)
	mux.HandleFunc("/authorize", h.HandleAuthorize)
	mux.HandleFunc("/token", h.HandleToken)
	mux.HandleFunc("/register", h.HandleRegister) // RFC 7591 Dynamic Client Registration
	// Note: /callback is already registered by kcManager, but we handle OAuth flow in it
}
