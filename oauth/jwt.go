// Package oauth provides a minimal OAuth implementation for kite-mcp-server.
// This replaces the fosite-based implementation with a simpler JWT-based approach.
//
// Key simplifications:
// - JWT tokens instead of opaque tokens (no introspection needed)
// - Public client registration via RFC 7591
// - No client credentials grant (user-facing only)
// - Leverages existing kc.SessionManager for Kite session binding
package oauth

import (
	"container/list"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// Config holds OAuth server configuration
type Config struct {
	// Issuer is the OAuth issuer URL (e.g., "https://mcp.kite.trade")
	Issuer string
	// JWTSecret is the secret for signing JWTs (must be 32+ bytes)
	JWTSecret []byte
	// TokenTTL is how long access tokens are valid
	TokenTTL time.Duration
	// SessionExpiry validates the backing grant and returns its fixed deadline.
	SessionExpiry func(string) (time.Time, error)
	// AuthCodeTTL is how long authorization codes are valid
	AuthCodeTTL time.Duration
	// TrustedProxyCIDRs identifies reverse proxies allowed to supply
	// X-Forwarded-For. With the default empty list, the direct peer is used.
	TrustedProxyCIDRs []string
}

// Server is the simplified OAuth server
type Server struct {
	config Config

	// In-memory stores (replace with Redis/DB for production)
	mu             sync.RWMutex
	authCodes      map[string]*AuthCode // code -> auth code data
	pkceVerifiers  map[string]string    // code -> code_verifier hash
	trustedProxies []netip.Prefix
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
		cfg.TokenTTL = 7 * 24 * time.Hour
	}
	if cfg.AuthCodeTTL == 0 {
		cfg.AuthCodeTTL = 10 * time.Minute
	}
	trustedProxies := make([]netip.Prefix, 0, len(cfg.TrustedProxyCIDRs))
	for _, raw := range cfg.TrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err == nil {
			trustedProxies = append(trustedProxies, prefix)
		}
	}
	return &Server{
		config:         cfg,
		authCodes:      make(map[string]*AuthCode),
		pkceVerifiers:  make(map[string]string),
		trustedProxies: trustedProxies,
	}
}

// --- Rate Limiting ---

// RateLimiter provides simple IP-based rate limiting
type RateLimiter struct {
	mu          sync.Mutex
	requests    map[string]*rateLimitEntry
	limit       int
	window      time.Duration
	lastCleanup time.Time
	peers       *list.List
}

const maxRateLimitPeers = 10000

type rateLimitEntry struct {
	requests   []time.Time
	lastAccess time.Time
	position   *list.Element
}

// rateLimitKey groups IPv6 privacy addresses by /64 and canonicalizes IPv4.
func rateLimitKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return addr.String()
}

// NewRateLimiter creates a rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string]*rateLimitEntry),
		peers:    list.New(),
		limit:    limit,
		window:   window,
	}
}

// Allow checks if request is within rate limit
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	ip = rateLimitKey(ip)
	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Remove expired IP entries and bound memory even when peers rotate addresses.
	if rl.lastCleanup.IsZero() || now.Sub(rl.lastCleanup) >= rl.window {
		for key, entry := range rl.requests {
			if !entry.lastAccess.After(cutoff) {
				delete(rl.requests, key)
				rl.peers.Remove(entry.position)
			}
		}
		rl.lastCleanup = now
	}
	entry, exists := rl.requests[ip]
	if !exists {
		if len(rl.requests) >= maxRateLimitPeers {
			// Evict the least recently used peer instead of rejecting unrelated users.
			oldest := rl.peers.Back()
			delete(rl.requests, oldest.Value.(string))
			rl.peers.Remove(oldest)
		}
		entry = &rateLimitEntry{position: rl.peers.PushFront(ip)}
		rl.requests[ip] = entry
	}
	rl.peers.MoveToFront(entry.position)
	entry.lastAccess = now

	// Filter old requests
	var recent []time.Time
	for _, t := range entry.requests {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.limit {
		return false
	}

	entry.requests = append(recent, now)
	return true
}

// --- Redirect URI Validation ---

// ValidateRedirectURI accepts HTTPS callbacks and HTTP loopback callbacks.
// Credentials and fragments are never valid redirect URI metadata.
func (s *Server) ValidateRedirectURI(redirectURI string) error {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return fmt.Errorf("invalid redirect_uri: %w", err)
	}
	if parsed.Hostname() == "" || parsed.User != nil || strings.Contains(redirectURI, "#") {
		return fmt.Errorf("invalid redirect_uri: hostname required; credentials and fragments forbidden")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := parsed.Hostname()
	ip, _ := netip.ParseAddr(host)
	if parsed.Scheme == "http" && (host == "localhost" || ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("invalid redirect_uri: HTTPS required except for loopback callbacks")
}

// Client IDs carry signed callback hashes instead of transient server metadata.
// They have no expiry: changing JWTSecret or Issuer invalidates registrations.
// A separate format and signing domain prevent confusion with access tokens.
const maxClientIDBytes = 1024

type clientRegistration struct {
	Nonce          string   `json:"nonce"`
	RedirectHashes []string `json:"redirect_hashes"`
}

func redirectHash(uri string) string {
	hash := sha256.Sum256([]byte(uri))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func (s *Server) clientSignature(payload string) []byte {
	mac := hmac.New(sha256.New, s.config.JWTSecret)
	mac.Write([]byte("kite-mcp-client-registration-v1\x00"))
	mac.Write([]byte(s.config.Issuer))
	mac.Write([]byte{0})
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// ValidateClientRedirect verifies a server-issued client ID and exact callback binding.
func (s *Server) ValidateClientRedirect(clientID, redirectURI string) error {
	if len(s.config.JWTSecret) < 32 || len(clientID) > maxClientIDBytes {
		return errors.New("invalid client registration")
	}
	parts := strings.Split(clientID, ".")
	if len(parts) != 3 || parts[0] != "dcr1" {
		return errors.New("invalid client registration")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, s.clientSignature(parts[1])) {
		return errors.New("invalid client registration")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("invalid client registration")
	}
	var registration clientRegistration
	if err := json.Unmarshal(payload, &registration); err != nil || registration.Nonce == "" || len(registration.RedirectHashes) == 0 || len(registration.RedirectHashes) > maxDCRRedirectURIs {
		return errors.New("invalid client registration")
	}
	hash := redirectHash(redirectURI)
	for _, registered := range registration.RedirectHashes {
		if registered == hash {
			return nil
		}
	}
	return errors.New("redirect_uri does not match client registration")
}

// RegisterClient issues a durable, signed registration without retaining metadata.
func (s *Server) RegisterClient(redirectURIs []string) (string, error) {
	if len(s.config.JWTSecret) < 32 {
		return "", errors.New("client signing secret must be at least 32 bytes")
	}
	if len(redirectURIs) == 0 || len(redirectURIs) > maxDCRRedirectURIs {
		return "", errors.New("invalid redirect URI count")
	}
	registration := clientRegistration{Nonce: generateSecureToken(16)}
	for _, uri := range redirectURIs {
		if len(uri) == 0 || len(uri) > maxDCRURIBytes {
			return "", errors.New("invalid redirect URI length")
		}
		if err := s.ValidateRedirectURI(uri); err != nil {
			return "", err
		}
		registration.RedirectHashes = append(registration.RedirectHashes, redirectHash(uri))
	}
	payload, err := json.Marshal(registration)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return "dcr1." + encoded + "." + base64.RawURLEncoding.EncodeToString(s.clientSignature(encoded)), nil
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
	expiresAt := now.Add(s.config.TokenTTL)
	if s.config.SessionExpiry != nil {
		deadline, err := s.config.SessionExpiry(sessionID)
		if err != nil {
			return "", err
		}
		if deadline.Before(expiresAt) {
			expiresAt = deadline
		}
	}
	if !expiresAt.Truncate(time.Second).After(now) {
		return "", errors.New("application grant has expired")
	}
	claims := TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.config.Issuer,
			Subject:   kiteUserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
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
	verifier := func(_ context.Context, token string, r *http.Request) (*auth.TokenInfo, error) {
		claims, err := s.ValidateToken(token)
		if err != nil || claims.ExpiresAt == nil {
			return nil, auth.ErrInvalidToken
		}
		if s.config.SessionExpiry != nil {
			if _, err := s.config.SessionExpiry(claims.SessionID); err != nil {
				return nil, auth.ErrInvalidToken
			}
		}
		// This is distinct from Mcp-Session-Id, which is owned by the SDK.
		r.Header.Set("X-Kite-Session-Id", claims.SessionID)
		return &auth.TokenInfo{
			UserID:     claims.KiteUserID,
			Expiration: claims.ExpiresAt.Time,
			Extra:      map[string]any{"kite_claims": claims},
		}, nil
	}
	return auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: s.config.Issuer + "/.well-known/oauth-protected-resource",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if tokenInfo := auth.TokenInfoFromContext(ctx); tokenInfo != nil {
			if claims, ok := tokenInfo.Extra["kite_claims"].(*TokenClaims); ok {
				ctx = context.WithValue(ctx, claimsKey{}, claims)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}

// ClientIP returns a proxied client address only when the direct peer belongs
// to a configured trusted proxy network. It walks X-Forwarded-For from the
// proxy side and returns the first untrusted hop.
func (s *Server) ClientIP(r *http.Request) string {
	peer, ok := parseRemoteAddr(r.RemoteAddr)
	if !ok {
		return r.RemoteAddr
	}
	if !s.isTrustedProxy(peer) {
		return peer.String()
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err == nil && !s.isTrustedProxy(hop) {
			return hop.String()
		}
	}
	return peer.String()
}

func parseRemoteAddr(value string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		value = host
	}
	addr, err := netip.ParseAddr(value)
	return addr, err == nil
}

func (s *Server) isTrustedProxy(addr netip.Addr) bool {
	for _, prefix := range s.trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

type claimsKey struct{}

// ClaimsFromContext extracts token claims from request context
func ClaimsFromContext(ctx context.Context) *TokenClaims {
	if claims, ok := ctx.Value(claimsKey{}).(*TokenClaims); ok {
		return claims
	}
	return nil
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
