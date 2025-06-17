package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
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
	ar, err := h.FositeProvider.NewAuthorizeRequest(ctx, r)
	if err != nil {
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

// Token is the handler for the /token endpoint.
func (h *OAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mySessionData := &fosite.DefaultSession{}
	accessRequest, err := h.FositeProvider.NewAccessRequest(ctx, r, mySessionData)
	if err != nil {
		h.FositeProvider.WriteAccessError(ctx, w, accessRequest, err)
		return
	}
	accessResponse, err := h.FositeProvider.NewAccessResponse(ctx, accessRequest)
	if err != nil {
		h.FositeProvider.WriteAccessError(ctx, w, accessRequest, err)
		return
	}
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
			Subject: userSession.ID,
			// The user suggested that an uninitialized ExpiresAt map might cause issues.
			// While Fosite's setters/getters are nil-safe, other parts of the library
			// or its dependencies might not be. Initializing it is a safe bet.
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

		h.Logger.Debug("Preparing to complete OAuth flow",
			"session_id", sessionID,
			"user_id", userSession.ID,
			"redirect_uri", ar.GetRedirectURI().String(),
			"state", ar.GetState(),
			"response_types", ar.GetResponseTypes(),
			"requested_scopes", ar.GetRequestedScopes(),
			"granted_scopes", ar.GetGrantedScopes(),
			"fosite_session_subject", mySession.Subject)

		response, err := h.FositeProvider.NewAuthorizeResponse(ctx, ar, mySession)
		if err != nil {
			// Log the full Fosite error for debugging, including debug and hint fields.
			if rfcErr, ok := err.(*fosite.RFC6749Error); ok {
				h.Logger.Error("Fosite failed to create authorize response", "error", rfcErr.Error(), "debug", rfcErr.Debug(), "hint", rfcErr.HintField, "session_id", sessionID)
			} else {
				h.Logger.Error("Fosite failed to create authorize response with a non-fosite error", "error", err, "session_id", sessionID)
			}
			h.FositeProvider.WriteAuthorizeError(ctx, w, ar, err)
			return
		}

		h.Logger.Debug("Fosite successfully created authorize response", "session_id", sessionID)
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

	h.FositeStore.AddClient(client)
	h.Logger.Info("Successfully registered new dynamic client", "client_id", client.ID)

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

// Middleware is the OAuth 2.1 middleware for protecting MCP endpoints.
func (h *OAuthHandler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token := fosite.AccessTokenFromRequest(r)
		if token == "" {
			http.Error(w, "Unauthorized: No access token provided", http.StatusUnauthorized)
			return
		}

		_, ar, err := h.FositeProvider.IntrospectToken(ctx, token, fosite.AccessToken, &fosite.DefaultSession{})
		if err != nil {
			http.Error(w, "Invalid or expired OAuth token", http.StatusUnauthorized)
			return
		}

		sessionID := ar.GetSession().GetSubject()

		// Check if the underlying Kite credentials are still valid.
		if _, err := h.KCManager.GetAuthenticatedClient(sessionID); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// Set the authenticated session ID in the header for the next handler.
		r.Header.Set("Mcp-Session-Id", sessionID)
		next.ServeHTTP(w, r)
	})
}
