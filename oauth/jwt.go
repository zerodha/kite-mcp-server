// Package oauth provides a minimal OAuth implementation for kite-mcp-server.
// This replaces the fosite-based implementation with a simpler JWT-based approach.
//
// Key simplifications:
// - JWT tokens instead of opaque tokens (no introspection needed)
// - Auto-registration only (no RFC 7591 dynamic registration)
// - No client credentials grant (user-facing only)
// - Leverages existing kc.SessionManager for Kite session binding
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Config holds OAuth server configuration
type Config struct {
	// Issuer is the OAuth issuer URL (e.g., "https://mcp.kite.trade")
	Issuer string
	// JWTSecret is the secret for signing JWTs (must be 32+ bytes)
	JWTSecret []byte
	// TokenTTL is how long access tokens are valid
	TokenTTL time.Duration
	// AuthCodeTTL is how long authorization codes are valid
	AuthCodeTTL time.Duration
	// AllowedRedirectPatterns defines allowed redirect URI patterns for DCR.
	// Default is "localhost", which matches http://localhost:* and http://127.0.0.1:*
	// for native/loopback clients. Additional entries must be exact redirect URIs,
	// e.g. https://claude.ai/api/mcp/auth_callback
	AllowedRedirectPatterns []string
}

// Server is the simplified OAuth server
type Server struct {
	config Config

	// In-memory stores (replace with Redis/DB for production)
	mu            sync.RWMutex
	clients       map[string]*Client   // client_id -> client
	authCodes     map[string]*AuthCode // code -> auth code data
	pkceVerifiers map[string]string    // code -> code_verifier hash
}

// Client represents an OAuth client (auto-registered)
type Client struct {
	ID           string
	RedirectURIs []string
	CreatedAt    time.Time
}

// AuthCode represents a pending authorization code
type AuthCode struct {
	Code        string
	ClientID    string
	RedirectURI string
	KiteUserID  string // From KiteConnect after login
	KiteSession string // Reference to kc.Session
	ExpiresAt   time.Time
	PKCEHash    string // S256 hash of code_verifier
}

// TokenClaims are the JWT claims for access tokens
type TokenClaims struct {
	jwt.RegisteredClaims
	// KiteUserID is the authenticated Kite user
	KiteUserID string `json:"kite_user_id"`
	// SessionID references the kc.Session with Kite credentials
	SessionID string `json:"session_id"`
	// Scope is the granted scope
	Scope string `json:"scope,omitempty"`
}

// New creates a new simplified OAuth server
func New(cfg Config) *Server {
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = 6 * time.Hour
	}
	if cfg.AuthCodeTTL == 0 {
		cfg.AuthCodeTTL = 10 * time.Minute
	}
	if len(cfg.AllowedRedirectPatterns) == 0 {
		cfg.AllowedRedirectPatterns = []string{"localhost"}
	}
	return &Server{
		config:        cfg,
		clients:       make(map[string]*Client),
		authCodes:     make(map[string]*AuthCode),
		pkceVerifiers: make(map[string]string),
	}
}

// --- Rate Limiting ---

// RateLimiter provides simple IP-based rate limiting
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

// NewRateLimiter creates a rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

// Allow checks if request is within rate limit
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Filter old requests
	var recent []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.limit {
		return false
	}

	rl.requests[ip] = append(recent, now)
	return true
}

// --- Redirect URI Validation ---

// ValidateRedirectURI checks if a redirect URI matches allowed patterns.
// Default behavior allows loopback redirects for native clients. Hosted clients
// like Claude Web and ChatGPT Web must be explicitly allowlisted by exact URI.
func (s *Server) ValidateRedirectURI(redirectURI string) error {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return fmt.Errorf("invalid redirect_uri: %w", err)
	}
	if parsed.Scheme == "" || parsed.Hostname() == "" {
		return fmt.Errorf("invalid redirect_uri: missing scheme or hostname")
	}

	for _, pattern := range s.config.AllowedRedirectPatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		switch pattern {
		case "localhost":
			host := parsed.Hostname()
			if (host == "localhost" || host == "127.0.0.1") &&
				(parsed.Scheme == "http" || parsed.Scheme == "https") {
				return nil
			}
		default:
			if redirectURI == pattern {
				return nil
			}
		}
	}

	return fmt.Errorf("redirect_uri not allowed: must match configured allowlist")
}

// --- Auto Registration ---

// GetOrCreateClient auto-registers a client if not found
func (s *Server) GetOrCreateClient(clientID, redirectURI string) *Client {
	s.mu.Lock()
	defer s.mu.Unlock()

	if client, ok := s.clients[clientID]; ok {
		// Add redirect URI if not already present
		for _, uri := range client.RedirectURIs {
			if uri == redirectURI {
				return client
			}
		}
		client.RedirectURIs = append(client.RedirectURIs, redirectURI)
		return client
	}

	// Auto-register new client
	client := &Client{
		ID:           clientID,
		RedirectURIs: []string{redirectURI},
		CreatedAt:    time.Now(),
	}
	s.clients[clientID] = client
	return client
}

// --- Authorization Endpoint ---

// AuthorizeRequest contains the /authorize request parameters
type AuthorizeRequest struct {
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string // PKCE
	CodeChallengeMethod string // Must be "S256"
	Scope               string
}

// ParseAuthorizeRequest parses an authorization request
func ParseAuthorizeRequest(r *http.Request) (*AuthorizeRequest, error) {
	q := r.URL.Query()

	clientID := q.Get("client_id")
	if clientID == "" {
		return nil, errors.New("missing client_id")
	}

	redirectURI := q.Get("redirect_uri")
	if redirectURI == "" {
		return nil, errors.New("missing redirect_uri")
	}

	// PKCE is required (OAuth 2.1)
	codeChallenge := q.Get("code_challenge")
	if codeChallenge == "" {
		return nil, errors.New("missing code_challenge (PKCE required)")
	}

	method := q.Get("code_challenge_method")
	if method != "S256" {
		return nil, errors.New("code_challenge_method must be S256")
	}

	return &AuthorizeRequest{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		State:               q.Get("state"),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: method,
		Scope:               q.Get("scope"),
	}, nil
}

// CreateAuthCode creates an authorization code after successful Kite login
func (s *Server) CreateAuthCode(clientID, redirectURI, kiteUserID, sessionID, pkceChallenge string) (string, error) {
	code := generateSecureToken(32)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.authCodes[code] = &AuthCode{
		Code:        code,
		ClientID:    clientID,
		RedirectURI: redirectURI,
		KiteUserID:  kiteUserID,
		KiteSession: sessionID,
		ExpiresAt:   time.Now().Add(s.config.AuthCodeTTL),
		PKCEHash:    pkceChallenge,
	}

	return code, nil
}

// --- Token Endpoint ---

// TokenRequest contains the /token request parameters
type TokenRequest struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	CodeVerifier string // PKCE
}

// ParseTokenRequest parses a token request
func ParseTokenRequest(r *http.Request) (*TokenRequest, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}

	grantType := r.FormValue("grant_type")
	if grantType != "authorization_code" {
		return nil, fmt.Errorf("unsupported grant_type: %s", grantType)
	}

	code := r.FormValue("code")
	if code == "" {
		return nil, errors.New("missing code")
	}

	codeVerifier := r.FormValue("code_verifier")
	if codeVerifier == "" {
		return nil, errors.New("missing code_verifier (PKCE required)")
	}

	return &TokenRequest{
		GrantType:    grantType,
		Code:         code,
		RedirectURI:  r.FormValue("redirect_uri"),
		ClientID:     r.FormValue("client_id"),
		CodeVerifier: codeVerifier,
	}, nil
}

// ExchangeCode exchanges an authorization code for tokens
func (s *Server) ExchangeCode(req *TokenRequest) (accessToken string, err error) {
	s.mu.Lock()
	authCode, ok := s.authCodes[req.Code]
	if ok {
		delete(s.authCodes, req.Code) // One-time use
	}
	s.mu.Unlock()

	if !ok {
		return "", errors.New("invalid authorization code")
	}

	// Validate expiration
	if time.Now().After(authCode.ExpiresAt) {
		return "", errors.New("authorization code expired")
	}

	// Validate client
	if authCode.ClientID != req.ClientID {
		return "", errors.New("client_id mismatch")
	}

	// Validate redirect URI
	if authCode.RedirectURI != req.RedirectURI {
		return "", errors.New("redirect_uri mismatch")
	}

	// Validate PKCE
	if !verifyPKCE(req.CodeVerifier, authCode.PKCEHash) {
		return "", errors.New("invalid code_verifier")
	}

	// Generate JWT access token
	return s.generateAccessToken(authCode.KiteUserID, authCode.KiteSession)
}

func (s *Server) generateAccessToken(kiteUserID, sessionID string) (string, error) {
	now := time.Now()
	claims := TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.config.Issuer,
			Subject:   kiteUserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.TokenTTL)),
			ID:        generateSecureToken(16),
		},
		KiteUserID: kiteUserID,
		SessionID:  sessionID,
		Scope:      "default",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.config.JWTSecret)
}

// --- Token Validation ---

// ValidateToken validates a JWT access token and returns the claims
func (s *Server) ValidateToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		// Pin to HS256 only - reject algorithm agility
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.config.JWTSecret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*TokenClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// --- Middleware ---

// Middleware returns HTTP middleware that validates Bearer tokens
func (s *Server) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			s.writeUnauthorized(w, "missing bearer token")
			return
		}

		tokenString := strings.TrimPrefix(auth, "Bearer ")
		claims, err := s.ValidateToken(tokenString)
		if err != nil {
			s.writeUnauthorized(w, "invalid or expired token")
			return
		}

		// Set custom header for tool handlers to read session ID
		// (don't use Mcp-Session-Id - that's managed by the SDK)
		r.Header.Set("X-Kite-Session-Id", claims.SessionID)

		// Add claims to context
		ctx := context.WithValue(r.Context(), claimsKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type claimsKey struct{}

// ClaimsFromContext extracts token claims from request context
func ClaimsFromContext(ctx context.Context) *TokenClaims {
	if claims, ok := ctx.Value(claimsKey{}).(*TokenClaims); ok {
		return claims
	}
	return nil
}

func (s *Server) writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer resource_metadata="%s/.well-known/oauth-protected-resource"`,
		s.config.Issuer,
	))
	http.Error(w, message, http.StatusUnauthorized)
}

// --- Discovery Endpoints ---

// AuthorizationServerMetadata returns RFC 8414 metadata
func (s *Server) AuthorizationServerMetadata() map[string]interface{} {
	return map[string]interface{}{
		"issuer":                                s.config.Issuer,
		"authorization_endpoint":                s.config.Issuer + "/authorize",
		"token_endpoint":                        s.config.Issuer + "/token",
		"registration_endpoint":                 s.config.Issuer + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"}, // Public clients
	}
}

// ProtectedResourceMetadata returns RFC 9728 metadata
func (s *Server) ProtectedResourceMetadata() map[string]interface{} {
	return map[string]interface{}{
		"resource":                 s.config.Issuer + "/mcp",
		"authorization_servers":    []string{s.config.Issuer},
		"scopes_supported":         []string{"default"},
		"bearer_methods_supported": []string{"header"},
	}
}

// --- Helpers ---

func generateSecureToken(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(bytes)
}

func verifyPKCE(codeVerifier, codeChallenge string) bool {
	// S256: BASE64URL(SHA256(code_verifier)) == code_challenge
	h := sha256.Sum256([]byte(codeVerifier))
	computed := base64.RawURLEncoding.EncodeToString(h[:])
	return computed == codeChallenge
}

// BuildRedirectWithCode builds the redirect URL with authorization code
func BuildRedirectWithCode(redirectURI, code, state string) string {
	u, _ := url.Parse(redirectURI)
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// WriteJSON writes a JSON response
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
