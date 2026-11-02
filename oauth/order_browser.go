package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type orderBrowserClaims struct {
	jwt.RegisteredClaims
	SessionID string `json:"session_id"`
	Purpose   string `json:"purpose"`
}

func (s *Server) orderBrowserCookie(sessionID string) (string, bool) {
	u, err := url.Parse(s.config.Issuer)
	secure := err != nil || u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1")
	prefix := ""
	if secure {
		prefix = "__Host-"
	}
	digest := sha256.Sum256([]byte(sessionID))
	return prefix + "kite-order-browser-" + hex.EncodeToString(digest[:12]), secure
}

// SetOrderBrowserSession establishes browser-only proof after Kite login. The
// OAuth client receives neither this cookie nor a token usable in its place.
func (s *Server) SetOrderBrowserSession(w http.ResponseWriter, sessionID, userID string) error {
	if s.config.SessionExpiry == nil {
		return errors.New("session validation unavailable")
	}
	deadline, err := s.config.SessionExpiry(sessionID)
	if err != nil {
		return err
	}
	claims := orderBrowserClaims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: s.config.Issuer, Subject: userID,
			Audience: jwt.ClaimStrings{s.config.Issuer + "/order-approvals"}, ExpiresAt: jwt.NewNumericDate(deadline)},
		SessionID: sessionID, Purpose: "order_browser",
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.config.JWTSecret)
	if err != nil {
		return err
	}
	name, secure := s.orderBrowserCookie(sessionID)
	http.SetCookie(w, &http.Cookie{Name: name, Value: token, Path: "/", Secure: secure,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: deadline, MaxAge: max(1, int(time.Until(deadline).Seconds()))})
	return nil
}

// VerifyOrderBrowserSession requires cookie proof for this exact grant. Bearer
// tokens, link possession, and MCP clientInfo are deliberately insufficient.
func (s *Server) VerifyOrderBrowserSession(r *http.Request, sessionID, userID string) error {
	if s.config.SessionExpiry == nil {
		return errors.New("session validation unavailable")
	}
	if _, err := s.config.SessionExpiry(sessionID); err != nil {
		return err
	}
	name, _ := s.orderBrowserCookie(sessionID)
	cookie, err := r.Cookie(name)
	if err != nil {
		return err
	}
	claims := &orderBrowserClaims{}
	token, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return s.config.JWTSecret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(s.config.Issuer),
		jwt.WithAudience(s.config.Issuer+"/order-approvals"), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.Purpose != "order_browser" || claims.SessionID != sessionID || claims.Subject != userID {
		return errors.New("browser sign-in required")
	}
	return nil
}

func (s *Server) IssuerURL() string { return s.config.Issuer }

// OrderBrowserCSRF binds the management form to this browser's grant proof.
func (s *Server) OrderBrowserCSRF(r *http.Request, sessionID string) string {
	name, _ := s.orderBrowserCookie(sessionID)
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, s.config.JWTSecret)
	mac.Write([]byte("order-permissions-csrf\x00" + cookie.Value))
	return hex.EncodeToString(mac.Sum(nil))
}
