package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ory/fosite"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/oauth"
)

// mustGenerateRandomString generates a random string of a given length or panics.
func mustGenerateRandomString(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

// OAuthHandler encapsulates all OAuth 2.1 HTTP handlers and their dependencies.
type OAuthHandler struct {
	FositeProvider fosite.OAuth2Provider
	FositeStore    *oauth.InMemoryStore
	KCManager      *kc.Manager
	Logger         *slog.Logger
	AppConfig      AppConfig
	clientRegMutex sync.Mutex
}

// AppConfig holds configuration required by the OAuth handlers.
type AppConfig struct {
	Host       string
	KiteAPIKey string
}

// NewOAuthHandler creates a new handler for all OAuth-related endpoints.
func NewOAuthHandler(provider fosite.OAuth2Provider, store *oauth.InMemoryStore, kcManager *kc.Manager, logger *slog.Logger, cfg AppConfig) *OAuthHandler {
	return &OAuthHandler{
		FositeProvider: provider,
		FositeStore:    store,
		KCManager:      kcManager,
		Logger:         logger,
		AppConfig:      cfg,
	}
}

// Authorize is the handler for the /authorize endpoint.
func (h *OAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")

	if clientID == "" || redirectURI == "" {
		http.Error(w, "Missing required parameters", http.StatusBadRequest)
		return
	}

	// Synchronized client lookup with potential auto-registration
	var err error

	// Try to get client - use a retry mechanism for race conditions
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		_, err := h.FositeStore.GetClient(ctx, clientID)
		if err == nil {
			// Client found successfully
			break
		}

		// Client not found - auto-register only on first attempt
		if attempt == 0 {
			// Auto-register as PUBLIC client (no secret required for PKCE flow)
			autoClient := &fosite.DefaultClient{
				ID:            clientID,
				Secret:        nil, // Public client - no secret
				GrantTypes:    fosite.Arguments{"authorization_code", "refresh_token"},
				ResponseTypes: fosite.Arguments{"code"},
				Scopes:        fosite.Arguments{"default", "openid"},
				RedirectURIs:  []string{redirectURI},
				Public:        true, // Mark as public client
			}

			// Atomic auto-registration
			h.FositeStore.AddClient(autoClient)

			// Immediate verification
			_, err = h.FositeStore.GetClient(ctx, clientID)
			if err == nil {
				h.Logger.Info("Auto-registered client", "client_id", clientID)
				break
			}
		}
	}

	// Create a new request with proper parameters
	newReq := r.Clone(ctx)
	query := newReq.URL.Query()

	// Add missing required parameters
	modified := false

	stateParam := query.Get("state")
	if stateParam == "" || len(stateParam) < 8 {
		state := "auto-state-" + mustGenerateRandomString(16)
		query.Set("state", state)
		modified = true
	}

	if query.Get("code_challenge") == "" && query.Get("response_type") == "code" {
		// Generate a proper PKCE challenge
		codeVerifier := mustGenerateRandomString(32)
		challenge := generateCodeChallenge(codeVerifier)
		query.Set("code_challenge", challenge)
		query.Set("code_challenge_method", "S256")
		modified = true
	}

	if modified {
		newReq.URL.RawQuery = query.Encode()
	}

	// Use the modified request
	ar, err := h.FositeProvider.NewAuthorizeRequest(ctx, newReq)
	if err != nil {
		h.Logger.Error("Failed to create authorize request", "error", err)
		h.FositeProvider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}

	// Generate a temporary session and store the authorize request in it.
	tempSessionID := h.KCManager.SessionManager().GenerateWithData(ar)

	// Generate a login URL that includes the signed temporary session ID.
	kiteLoginURL, err := h.KCManager.GenerateLoginURL(tempSessionID)
	if err != nil {
		http.Error(w, "Internal Server Error: failed to generate login URL", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, kiteLoginURL, http.StatusFound)
}

// Add this helper function for PKCE
func generateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(h[:])
}

// Token is the handler for the /token endpoint.
func (h *OAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mySessionData := &fosite.DefaultSession{}
	accessRequest, err := h.FositeProvider.NewAccessRequest(ctx, r, mySessionData)
	if err != nil {
		h.Logger.Error("Failed to create access request", "error", err, "error_type", fmt.Sprintf("%T", err))

		// Check if it's a client authentication error
		fositeErr := fosite.ErrorToRFC6749Error(err)
		if fositeErr.ErrorField == fosite.ErrInvalidClient.ErrorField {
			clientID := r.Form.Get("client_id")
			h.Logger.Error("Client authentication failed in token endpoint",
				"client_id", clientID,
				"error_description", fositeErr.DescriptionField)

			// Check if client exists
			if client, getErr := h.FositeStore.GetClient(ctx, clientID); getErr != nil {
				h.Logger.Error("Client not found in store during token exchange", "client_id", clientID)
			} else {
				h.Logger.Info("Client found in store, but authentication failed",
					"client_id", clientID,
					"redirect_uris", client.GetRedirectURIs())
			}
		}

		h.FositeProvider.WriteAccessError(ctx, w, accessRequest, err)
		return
	}

	h.Logger.Info("Access request created successfully, generating response")

	accessResponse, err := h.FositeProvider.NewAccessResponse(ctx, accessRequest)
	if err != nil {
		h.Logger.Error("Failed to create access response", "error", err)
		h.FositeProvider.WriteAccessError(ctx, w, accessRequest, err)
		return
	}

	h.Logger.Info("Token exchange completed successfully")
	h.FositeProvider.WriteAccessResponse(ctx, w, accessRequest, accessResponse)
}

// Callback is the handler for the /callback endpoint from Kite.
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m := h.KCManager

	q := r.URL.Query()
	requestToken := q.Get("request_token")
	signedSessionID := q.Get("session_id")

	// 1. Verify the session ID from the redirect.
	sessionID, err := m.SessionSigner().VerifySessionID(signedSessionID)
	if err != nil {
		http.Error(w, "Invalid or expired callback session", http.StatusBadRequest)
		return
	}

	// 2. Retrieve the session.
	session, err := m.SessionManager().Get(sessionID)
	if err != nil {
		http.Error(w, "Callback session not found", http.StatusInternalServerError)
		return
	}

	// 3. Complete the Kite login to get new credentials.
	creds, err := m.CompleteLogin(requestToken)
	if err != nil {
		// If it's an OAuth flow, write a Fosite error.
		if ar, ok := session.OAuthData.(fosite.AuthorizeRequester); ok {
			h.FositeProvider.WriteAuthorizeError(ctx, w, ar, err)
		} else {
			http.Error(w, "Failed to complete Kite login", http.StatusInternalServerError)
		}
		return
	}

	// 4. Store the new credentials in the session.
	session.Credentials = creds

	// 5. Route based on the session's original purpose.

	// FLOW 1: OAuth /authorize
	if ar, ok := session.OAuthData.(fosite.AuthorizeRequester); ok {
		h.Logger.Info("Handling OAuth callback", "session_id", sessionID)
		// This was a temporary session for OAuth. We will use the UserID from the
		// new credentials to create or find the user's permanent session.
		defer m.SessionManager().Terminate(sessionID)

		userSession, _, err := m.SessionManager().GetOrCreate(creds.UserID)
		if err != nil {
			http.Error(w, "Failed to create user session after OAuth login", http.StatusInternalServerError)
			return
		}
		// Store the fresh credentials in the user's persistent session.
		userSession.Credentials = creds

		// Complete the OAuth flow, which will redirect to the client
		// We initialize a new session for the user here. The user's ID will be the subject.
		mySession := &fosite.DefaultSession{
			Subject:   userSession.ID,
			ExpiresAt: make(map[fosite.TokenType]time.Time),
		}

		// If no scopes were requested, we grant a default scope.
		// Fosite requires at least one scope to be granted.
		if len(ar.GetRequestedScopes()) == 0 {
			ar.GrantScope("default")
		} else {
			for _, scope := range ar.GetRequestedScopes() {
				ar.GrantScope(scope)
			}
		}

		response, err := h.FositeProvider.NewAuthorizeResponse(ctx, ar, mySession)
		if err != nil {
			h.Logger.Error("Fosite failed to create authorize response", "error", err, "session_id", sessionID)
			h.FositeProvider.WriteAuthorizeError(ctx, w, ar, err)
			return
		}

		h.FositeProvider.WriteAuthorizeResponse(ctx, w, ar, response)
		return
	}

	// FLOW 2: Login Tool
	h.Logger.Info("Handling Login Tool callback", "session_id", sessionID)
	// This was a persistent session. We have already stored the new credentials in it.
	// Now, just render the success page for the user.
	if err := h.KCManager.RenderSuccessTemplate(w); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// Register is the handler for the /register endpoint.
func (h *OAuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	client := &fosite.DefaultClient{
		GrantTypes:    fosite.Arguments{"authorization_code", "refresh_token", "client_credentials"},
		ResponseTypes: fosite.Arguments{"code", "id_token"},
		Scopes:        fosite.Arguments{"openid", "offline"},
	}

	// Read and parse body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}

	var registrationRequest struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		GrantTypes   []string `json:"grant_types"`
	}

	if err := json.Unmarshal(body, &registrationRequest); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
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

	ctx := context.Background()

	// Store client and immediately verify in a synchronized block
	h.FositeStore.AddClient(client)

	// Immediate verification without delays - this forces synchronization
	storedClient, err := h.FositeStore.GetClient(ctx, client.ID)
	if err != nil {
		// If verification fails, remove the partially stored client
		h.FositeStore.DeleteClient(ctx, client.ID)
		http.Error(w, "Failed to register client", http.StatusInternalServerError)
		return
	}

	// check that the stored client has the right data
	if storedClient.GetID() != client.ID {
		h.FositeStore.DeleteClient(ctx, client.ID)
		http.Error(w, "Client registration verification failed", http.StatusInternalServerError)
		return
	}

	// Send response only after successful verification
	response := map[string]interface{}{
		"client_id":                client.GetID(),
		"client_secret":            secret,
		"grant_types":              client.GetGrantTypes(),
		"redirect_uris":            client.GetRedirectURIs(),
		"client_name":              registrationRequest.ClientName,
		"client_id_issued_at":      time.Now().Unix(),
		"client_secret_expires_at": 0,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		// Client is already stored, so don't delete it
		// Just log the encoding error
		h.Logger.Error("Failed to encode registration response", "error", err)
		return
	}

	// Optional: minimal logging after everything is done
	h.Logger.Info("Client registered successfully", "client_id", client.ID)
}

// Discovery is the handler for the /.well-known/oauth-authorization-server endpoint.
func (h *OAuthHandler) Discovery(w http.ResponseWriter, r *http.Request) {
	issuer := "http://" + h.AppConfig.Host // Should be https in production
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

// ProtectedResourceMetadata is the handler for the /.well-known/oauth-protected-resource endpoint.
// It provides clients with information about how to obtain an access token for the MCP server.
func (h *OAuthHandler) ProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	// As per RFC 9728, this endpoint provides metadata about the resource server (this MCP server).
	issuer := "http://" + h.AppConfig.Host // Should be https in production
	response := map[string]interface{}{
		"resource": "http://" + h.AppConfig.Host + "/mcp",
		"authorization_servers": []string{
			issuer,
		},
		"scopes_supported":         []string{"default", "offline", "openid"},
		"bearer_methods_supported": []string{"header"},
	}

	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	json.NewEncoder(w).Encode(response)
}

// Middleware is the OAuth 2.1 middleware for protecting MCP endpoints.
func (h *OAuthHandler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token := fosite.AccessTokenFromRequest(r)
		if token == "" {
			h.Logger.Error("No access token provided")
			h.writeUnauthorizedError(w, "Unauthorized: No access token provided")
			return
		}

		_, ar, err := h.FositeProvider.IntrospectToken(ctx, token, fosite.AccessToken, &fosite.DefaultSession{})
		if err != nil {
			h.writeUnauthorizedError(w, "Invalid or expired OAuth token")
			return
		}

		sessionID := ar.GetSession().GetSubject()

		// Check if the underlying Kite credentials are still valid.
		if _, err := h.KCManager.GetAuthenticatedClient(sessionID); err != nil {
			h.writeUnauthorizedError(w, err.Error())
			return
		}
		h.Logger.Info("All validation passed, forwarding to MCP handler")

		// Set the authenticated session ID in the header for the next handler.
		r.Header.Set("Mcp-Session-Id", sessionID)
		// Don't set response headers here, let the MCP handler do it
		next.ServeHTTP(w, r)
	})
}

// writeUnauthorizedError sets the WWW-Authenticate header and writes a 401 error.
func (h *OAuthHandler) writeUnauthorizedError(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="kite-mcp", error="invalid_token"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	errorResponse := map[string]interface{}{
		"error":                 "invalid_token",
		"error_description":     message,
		"authorization_servers": []string{"http://" + h.AppConfig.Host},
		"resource_metadata":     "/.well-known/oauth-protected-resource",
	}

	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		h.Logger.Error("Failed to encode error response", "error", err)
	}
}
