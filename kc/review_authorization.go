package kc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const ReviewAuthorizationPrefix = "kitemcp-review-"
const reviewAuthorizationDomain = "kite-mcp/review-authorization/v1"
const maxReviewAuthorizationBytes = 4096

func (s *SessionSigner) reviewCipher() (cipher.AEAD, error) {
	mac := hmac.New(sha256.New, s.secretKey)
	_, _ = mac.Write([]byte(reviewAuthorizationDomain))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// SealReviewAuthorization carries confidential, authenticated login state in
// the callback URL. The caller must include and validate its fixed deadline.
func (s *SessionSigner) SealReviewAuthorization(data []byte) (string, error) {
	if len(data) > maxReviewAuthorizationBytes {
		return "", errors.New("review authorization is too large")
	}
	aead, err := s.reviewCipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, data, []byte(reviewAuthorizationDomain))
	return ReviewAuthorizationPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *SessionSigner) OpenReviewAuthorization(token string) ([]byte, error) {
	if !strings.HasPrefix(token, ReviewAuthorizationPrefix) || len(token) > 2*maxReviewAuthorizationBytes {
		return nil, errors.New("invalid review authorization")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, ReviewAuthorizationPrefix))
	if err != nil {
		return nil, errors.New("invalid review authorization")
	}
	aead, err := s.reviewCipher()
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("invalid review authorization")
	}
	return aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(reviewAuthorizationDomain))
}
