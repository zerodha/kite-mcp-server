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
func (h *OAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ar, err := h.FositeProvider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		h.FositeProvider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}

	// Generate a temporary session ID for this OAuth flow.
	tempSessionID := h.KCManager.GenerateOAuthSessionID()

	// Store the authorize request in the temporary session.
	if err := h.KCManager.SessionManager().UpdateSessionData(tempSessionID, ar); err != nil {
		http.Error(w, "Internal Server Error: failed to store session", http.StatusInternalServerError)
		return
	}

	// Generate a login URL that includes the signed temporary session ID.
	kiteLoginURL, err := h.KCManager.GenerateOAuthLoginURL(tempSessionID)
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

	// 3. Route based on the data type stored in the session.

	// FLOW 1: OAuth /authorize - Session data is an AuthorizeRequester
	if ar, ok := session.Data.(fosite.AuthorizeRequester); ok {
		h.Logger.Info("Handling OAuth callback", "session_id", sessionID)
		// This is a temporary session. Clean it up after we're done.
		defer m.ClearSession(sessionID)

		// Use a temporary client to complete the login and get the UserID
		tempKSD := kc.NewKiteConnect(h.AppConfig.KiteAPIKey)
		tempKiteData := &kc.KiteSessionData{Kite: tempKSD}
		userSess, err := m.CompleteLogin(tempKiteData, requestToken)
		if err != nil {
			h.FositeProvider.WriteAuthorizeError(ctx, w, ar, err)
			return
		}

		// Complete the OAuth flow, which will redirect to the client
		mySession := &fosite.DefaultSession{Subject: userSess.UserID}
		for _, scope := range ar.GetGrantedScopes() {
			ar.GrantScope(scope)
		}
		response, err := h.FositeProvider.NewAuthorizeResponse(ctx, ar, mySession)
		if err != nil {
			h.FositeProvider.WriteAuthorizeError(ctx, w, ar, err)
			return
		}
		h.FositeProvider.WriteAuthorizeResponse(ctx, w, ar, response)
		return
	}

	// FLOW 2: Login Tool - Session data is KiteSessionData
	if ksd, ok := session.Data.(*kc.KiteSessionData); ok {
		h.Logger.Info("Handling Login Tool callback", "session_id", sessionID)
		// This is a persistent session. We complete the login directly into its KiteSessionData.
		_, err := m.CompleteLogin(ksd, requestToken)
		if err != nil {
			http.Error(w, "Failed to complete Kite login", http.StatusInternalServerError)
			return
		}

		// Render the success page for the user
		if err := h.KCManager.RenderSuccessTemplate(w); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// If we get here, the session data is of an unknown or nil type. This is an error.
	h.Logger.Error("Callback session data has unexpected type or is nil", "session_id", sessionID)
	http.Error(w, "Invalid callback context", http.StatusBadRequest)
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
			// Send 401 Unauthorized if no token is provided.
			http.Error(w, "Unauthorized: No access token provided", http.StatusUnauthorized)
			return
		}

		_, ar, err := h.FositeProvider.IntrospectToken(ctx, token, fosite.AccessToken, &fosite.DefaultSession{})
		if err != nil {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}
		// Set the authenticated session ID in the header for the next handler.
		r.Header.Set("Mcp-Session-Id", ar.GetSession().GetSubject())

		// The downstream handler will use the Mcp-Session-Id header, which we have
		// populated from the validated OAuth token's subject (the Kite User ID).
		next.ServeHTTP(w, r)
	})
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
