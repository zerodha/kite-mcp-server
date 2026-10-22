package oauth

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zerodha/kite-mcp-server/kc"
)

// Handlers wraps the OAuth server with HTTP handlers
type Handlers struct {
	server         *Server
	kcManager      *kc.Manager
	registerMinute *RateLimiter
	registerHour   *RateLimiter
	logger         *slog.Logger
}

// NewHandlers creates OAuth HTTP handlers
func NewHandlers(server *Server, kcManager *kc.Manager, logger *slog.Logger) *Handlers {
	return &Handlers{
		server:         server,
		registerMinute: NewRateLimiter(10, time.Minute),
		registerHour:   NewRateLimiter(100, time.Hour),
		kcManager:      kcManager,
		logger:         logger,
	}
}

// pendingAuth stores the OAuth request while user authenticates with Kite
type pendingAuth struct {
	ClientID      string
	RedirectURI   string
	State         string
	CodeChallenge string
	Scope         string
	mu            sync.Mutex
	csrf          string
	browserState  string
	approved      bool
	consumed      bool
}

// HandleAuthorize handles GET /authorize
func (h *Handlers) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	consentHeaders(w)
	if r.Method == http.MethodPost {
		h.handleConsent(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Parse the OAuth request
	req, err := ParseAuthorizeRequest(r)
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := h.server.ValidateResource(req.Resource); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_target"})
		return
	}

	// Validate redirect URI syntax before checking the registered callback
	if err := h.server.ValidateRedirectURI(req.RedirectURI); err != nil {
		h.logger.Warn("rejected redirect_uri", "redirect_uri", req.RedirectURI, "error", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Require a signed DCR client ID and its exact registered callback.
	if err := h.server.ValidateClientRedirect(req.ClientID, req.RedirectURI); err != nil {
		http.Error(w, "invalid client or redirect_uri", http.StatusBadRequest)
		return
	}

	// Store OAuth request in session for callback
	pending := &pendingAuth{
		ClientID:      req.ClientID,
		RedirectURI:   req.RedirectURI,
		State:         req.State,
		CodeChallenge: req.CodeChallenge,
		Scope:         req.Scope,
		csrf:          generateSecureToken(32),
	}

	// Generate a temporary session to track this OAuth flow
	tempSessionID := h.kcManager.SessionManager().GenerateWithData(pending)

	// This cookie protects the consent form only. The callback state cookie
	// is created only after explicit approval, immediately before Kite login.
	h.setFlowCookie(w, tempSessionID, "consent", pending.csrf, 600)
	clientName, err := h.server.ClientName(req.ClientID)
	if err != nil {
		http.Error(w, "invalid client", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(clientName) == "" {
		clientName = "Unnamed MCP client"
	}
	if err := h.kcManager.RenderClientConsent(w, clientName, req.RedirectURI, tempSessionID, pending.csrf); err != nil {
		h.logger.Error("failed to render consent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}

}

// Consent is deliberately requested for every flow; no broad remembered approval
// can authorize a newly registered client. Only server-held pending data is used.
func (h *Handlers) handleConsent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	flow := r.PostForm.Get("flow")
	session, err := h.kcManager.SessionManager().Get(flow)
	if err != nil || session.Terminated || !time.Now().Before(session.ExpiresAt) {
		http.Error(w, "consent expired", http.StatusBadRequest)
		return
	}
	pending, ok := session.OAuthData.(*pendingAuth)
	if !ok {
		http.Error(w, "invalid consent", http.StatusBadRequest)
		return
	}
	cookie, err := r.Cookie(h.flowCookieName(flow, "consent"))
	pending.mu.Lock()
	defer pending.mu.Unlock()
	if err != nil || pending.approved || pending.consumed || !sameSecret(cookie.Value, pending.csrf) || !sameSecret(r.PostForm.Get("csrf"), pending.csrf) {
		http.Error(w, "invalid consent", http.StatusBadRequest)
		return
	}
	h.setFlowCookie(w, flow, "consent", "", -1)
	if err := h.server.ValidateClientRedirect(pending.ClientID, pending.RedirectURI); err != nil {
		http.Error(w, "invalid client", http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("decision") != "approve" {
		pending.consumed = true
		_, _ = h.kcManager.SessionManager().Terminate(flow)
		callback, _ := url.Parse(pending.RedirectURI)
		q := callback.Query()
		q.Set("error", "access_denied")
		if pending.State != "" {
			q.Set("state", pending.State)
		}
		callback.RawQuery = q.Encode()
		http.Redirect(w, r, callback.String(), http.StatusSeeOther)
		return
	}
	pending.browserState = generateSecureToken(32)
	loginURL, err := h.kcManager.GenerateLoginURL(flow, pending.browserState)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	pending.approved = true
	h.setFlowCookie(w, flow, "state", pending.browserState, 600)
	http.Redirect(w, r, loginURL, http.StatusSeeOther)
}

func sameSecret(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func consentHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	// form-action also governs form redirect chains in Chromium. The validated
	// OAuth redirects leave this origin; CSRF is enforced by the browser-bound
	// token, while framing and base-URL restrictions remain in place.
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'none'")
}

func (h *Handlers) secureCookies() bool {
	issuer, err := url.Parse(h.server.config.Issuer)
	if err != nil {
		return true
	}
	host := issuer.Hostname()
	return !(issuer.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1"))
}

func (h *Handlers) flowCookieName(flow, purpose string) string {
	prefix := "__Host-"
	if !h.secureCookies() {
		prefix = ""
	} // HTTP loopback development only.
	return prefix + "kite-" + purpose + "-" + strings.TrimPrefix(flow, "kitemcp-")
}

func (h *Handlers) setFlowCookie(w http.ResponseWriter, flow, purpose, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: h.flowCookieName(flow, purpose), Value: value, Path: "/", Secure: h.secureCookies(), HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

// callbackRateLimiter limits /callback requests (30 per IP per minute)
var callbackRateLimiter = NewRateLimiter(30, time.Minute)

// HandleCallback handles GET /callback from KiteConnect
func (h *Handlers) HandleCallback(w http.ResponseWriter, r *http.Request) {
	consentHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientIP := h.server.ClientIP(r)
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

	// Verify explicit consent and the browser binding before exchanging any Kite
	// credential. Claim the callback once, including when upstream login fails.
	pending, isOAuth := session.OAuthData.(*pendingAuth)
	if session.Terminated || !time.Now().Before(session.ExpiresAt) {
		http.Error(w, "authorization expired", http.StatusBadRequest)
		return
	}
	if isOAuth {
		cookie, cookieErr := r.Cookie(h.flowCookieName(sessionID, "state"))
		pending.mu.Lock()
		valid := cookieErr == nil && pending.approved && !pending.consumed && sameSecret(cookie.Value, pending.browserState) && sameSecret(q.Get("state"), pending.browserState)
		if valid {
			pending.consumed = true
		}
		pending.mu.Unlock()
		if !valid {
			http.Error(w, "authorization browser mismatch or consent missing", http.StatusBadRequest)
			return
		}
		h.setFlowCookie(w, sessionID, "state", "", -1)
		defer func() { _, _ = h.kcManager.SessionManager().Terminate(sessionID) }()
	}
	// Complete KiteConnect login
	creds, err := h.kcManager.CompleteLogin(requestToken)
	if err != nil {
		h.logger.Error("failed to complete Kite login", "error", err)
		http.Error(w, "authorization failed", http.StatusInternalServerError)
		return
	}

	// Check if this is an OAuth flow or regular login tool flow
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

	// OAuth flow - clean up the temporary state and create a fresh, opaque
	// grant session. Reusing the Kite user ID here would let a later login
	// recreate an identifier revoked by logout and revive an old JWT.
	userSessionID := h.kcManager.SessionManager().Generate()

	// Update credentials in the user session
	if err := h.kcManager.SessionManager().UpdateCredentials(userSessionID, creds); err != nil {
		_, _ = h.kcManager.SessionManager().Terminate(userSessionID)
		h.logger.Error("failed to update user session credentials", "error", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	// Generate authorization code
	code, err := h.server.CreateAuthCode(
		pending.ClientID,
		pending.RedirectURI,
		creds.UserID,
		userSessionID,
		pending.CodeChallenge,
	)
	if err != nil {
		_, _ = h.kcManager.SessionManager().Terminate(userSessionID)
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

	if err := h.server.ValidateResource(req.Resource); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_target"})
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

	// Report the actual remaining JWT lifetime, which may be shorter than the
	// configured maximum when authorization-code exchange was delayed.
	claims, err := h.server.ValidateToken(accessToken)
	if err != nil || claims.ExpiresAt == nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	h.logger.Info("token issued", "client_id", req.ClientID)
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   max(0, int(time.Until(claims.ExpiresAt.Time).Seconds())),
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

// RegisterRequest represents RFC 7591 client registration request
type RegisterRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
	ClientName   string   `json:"client_name,omitempty"`
}

const (
	maxDCRBodyBytes    = 16 << 10
	maxDCRRedirectURIs = 10
	maxDCRURIBytes     = 2048
	maxDCRClientName   = 256
)

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

	clientIP := h.server.ClientIP(r)
	if !h.registerHour.Allow(clientIP) {
		w.Header().Set("Retry-After", "3600")
		WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limit_exceeded", "error_description": "Too many registration requests. Try again later."})
		return
	}
	if !h.registerMinute.Allow(clientIP) {
		w.Header().Set("Retry-After", "60")
		WriteJSON(w, http.StatusTooManyRequests, map[string]string{
			"error":             "rate_limit_exceeded",
			"error_description": "Too many registration requests. Try again later.",
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDCRBodyBytes)
	var req RegisterRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_client_metadata",
			"error_description": "Invalid request body.",
		})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_client_metadata",
			"error_description": "Invalid request body.",
		})
		return
	}

	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > maxDCRRedirectURIs || len(req.ClientName) > maxDCRClientName {
		WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_client_metadata",
			"error_description": "redirect_uris is required",
		})
		return
	}

	// Validate all redirect URIs
	for _, uri := range req.RedirectURIs {
		if len(uri) == 0 || len(uri) > maxDCRURIBytes {
			WriteJSON(w, http.StatusBadRequest, map[string]string{
				"error":             "invalid_client_metadata",
				"error_description": "Invalid redirect URI metadata.",
			})
			return
		}
		if err := h.server.ValidateRedirectURI(uri); err != nil {
			h.logger.Warn("rejected DCR redirect_uri", "redirect_uri", uri, "error", err)
			WriteJSON(w, http.StatusBadRequest, map[string]string{
				"error":             "invalid_redirect_uri",
				"error_description": "Use an HTTPS redirect URI or an HTTP loopback callback.",
			})
			return
		}
	}

	clientID, err := h.server.RegisterClient(req.RedirectURIs, req.ClientName)
	if err != nil {
		h.logger.Error("failed to register client", "error", err)
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}

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
