package kc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidSignature = errors.New("invalid session signature")
	ErrTamperedSession  = errors.New("session parameter has been tampered with")
	ErrExpiredSignature = errors.New("session signature has expired")
	ErrInvalidFormat    = errors.New("invalid session parameter format")
)

const (
	// Default expiry for signed session parameters (30 minutes)
	DefaultSignatureExpiry = 30 * time.Minute

	// Maximum allowed clock skew for signature validation
	MaxClockSkew = 5 * time.Minute
)

// SessionSigner handles HMAC signing and verification of session parameters
type SessionSigner struct {
	secretKey       []byte
	signatureExpiry time.Duration
}

// NewSessionSigner creates a new session signer with a random secret key
func NewSessionSigner() (*SessionSigner, error) {
	secretKey := make([]byte, 32) // 256-bit key
	if _, err := rand.Read(secretKey); err != nil {
		return nil, fmt.Errorf("failed to generate secret key: %w", err)
	}

	return &SessionSigner{
		secretKey:       secretKey,
		signatureExpiry: DefaultSignatureExpiry,
	}, nil
}

// NewSessionSignerWithKey creates a new session signer with a provided secret key
func NewSessionSignerWithKey(secretKey []byte) *SessionSigner {
	if len(secretKey) == 0 {
		panic("secret key cannot be empty")
	}

	return &SessionSigner{
		secretKey:       secretKey,
		signatureExpiry: DefaultSignatureExpiry,
	}
}

// SetSignatureExpiry sets the expiry duration for signed session parameters
func (s *SessionSigner) SetSignatureExpiry(duration time.Duration) {
	s.signatureExpiry = duration
}

// SignSessionID creates a signed session parameter with timestamp and HMAC signature
func (s *SessionSigner) SignSessionID(sessionID string) string {
	timestamp := time.Now().Unix()

	// Create the payload: sessionID|timestamp
	payload := fmt.Sprintf("%s|%d", sessionID, timestamp)

	// Generate HMAC signature
	h := hmac.New(sha256.New, s.secretKey)
	h.Write([]byte(payload))
	signature := h.Sum(nil)

	// Encode signature as base64
	encodedSig := base64.URLEncoding.EncodeToString(signature)

	// Return format: payload.signature
	return fmt.Sprintf("%s.%s", payload, encodedSig)
}

// VerifySessionID verifies a signed session parameter and extracts the session ID
func (s *SessionSigner) VerifySessionID(signedParam string) (string, error) {
	// Split into payload and signature
	parts := strings.Split(signedParam, ".")
	if len(parts) != 2 {
		return "", ErrInvalidFormat
	}

	payload := parts[0]
	providedSig := parts[1]

	// Decode the provided signature
	decodedSig, err := base64.URLEncoding.DecodeString(providedSig)
	if err != nil {
		return "", fmt.Errorf("%w: invalid base64 encoding", ErrInvalidSignature)
	}

	// Generate expected signature
	h := hmac.New(sha256.New, s.secretKey)
	h.Write([]byte(payload))
	expectedSig := h.Sum(nil)

	// Verify signature using constant-time comparison
	if !hmac.Equal(decodedSig, expectedSig) {
		return "", ErrTamperedSession
	}

	// Parse payload: sessionID|timestamp
	payloadParts := strings.Split(payload, "|")
	if len(payloadParts) != 2 {
		return "", ErrInvalidFormat
	}

	sessionID := payloadParts[0]
	timestampStr := payloadParts[1]

	// Parse and validate timestamp
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("%w: invalid timestamp", ErrInvalidFormat)
	}

	// Check if signature has expired
	signatureTime := time.Unix(timestamp, 0)
	now := time.Now()

	if now.Sub(signatureTime) > s.signatureExpiry+MaxClockSkew {
		return "", ErrExpiredSignature
	}

	// Check for future timestamps (with clock skew tolerance)
	if signatureTime.Sub(now) > MaxClockSkew {
		return "", ErrInvalidSignature
	}

	return sessionID, nil
}

// ValidateSessionID performs basic validation on a session ID format
func (s *SessionSigner) ValidateSessionID(sessionID string) error {
	return checkSessionID(sessionID)
}

// SignRedirectParams creates a signed redirect parameter string for Kite authentication
func (s *SessionSigner) SignRedirectParams(sessionID string) (string, error) {
	if err := s.ValidateSessionID(sessionID); err != nil {
		return "", fmt.Errorf("invalid session ID: %w", err)
	}

	signedSessionID := s.SignSessionID(sessionID)
	return fmt.Sprintf("session_id=%s", signedSessionID), nil
}

// VerifyRedirectParams verifies signed redirect parameters and extracts the session ID
func (s *SessionSigner) VerifyRedirectParams(redirectParams string) (string, error) {
	// Parse session_id parameter
	if !strings.HasPrefix(redirectParams, "session_id=") {
		return "", ErrInvalidFormat
	}

	signedSessionID := strings.TrimPrefix(redirectParams, "session_id=")
	if signedSessionID == "" {
		return "", ErrInvalidFormat
	}

	return s.VerifySessionID(signedSessionID)
}

// GetSecretKey returns the secret key (for testing purposes only)
func (s *SessionSigner) GetSecretKey() []byte {
	// Return a copy to prevent external modification
	key := make([]byte, len(s.secretKey))
	copy(key, s.secretKey)
	return key
}
