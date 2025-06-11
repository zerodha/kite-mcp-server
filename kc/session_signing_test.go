//go:build goexperiment.synctest

package kc

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestNewSessionSigner(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create session signer: %v", err)
	}

	if len(signer.secretKey) != 32 {
		t.Errorf("Expected secret key length 32, got %d", len(signer.secretKey))
	}

	if signer.signatureExpiry != DefaultSignatureExpiry {
		t.Errorf("Expected default expiry %v, got %v", DefaultSignatureExpiry, signer.signatureExpiry)
	}
}

func TestNewSessionSignerWithKey(t *testing.T) {
	secretKey := []byte("test-secret-key-32-bytes-long!!")
	signer := NewSessionSignerWithKey(secretKey)

	if len(signer.secretKey) != len(secretKey) {
		t.Errorf("Expected secret key length %d, got %d", len(secretKey), len(signer.secretKey))
	}

	// Test panic with empty key
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic with empty secret key")
		}
	}()
	NewSessionSignerWithKey([]byte{})
}

func TestSignAndVerifySessionID(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create session signer: %v", err)
	}

	testSessionID := "kitemcp-550e8400-e29b-41d4-a716-446655440000"

	// Sign the session ID
	signed := signer.SignSessionID(testSessionID)

	// Verify the signed session ID
	verifiedSessionID, err := signer.VerifySessionID(signed)
	if err != nil {
		t.Fatalf("Failed to verify session ID: %v", err)
	}

	if verifiedSessionID != testSessionID {
		t.Errorf("Expected session ID %s, got %s", testSessionID, verifiedSessionID)
	}
}

func TestSignedSessionIDFormat(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create session signer: %v", err)
	}

	sessionID := "kitemcp-550e8400-e29b-41d4-a716-446655440000"
	signed := signer.SignSessionID(sessionID)

	// Should have format: sessionID|timestamp.signature
	parts := strings.Split(signed, ".")
	if len(parts) != 2 {
		t.Errorf("Expected 2 parts separated by '.', got %d parts", len(parts))
	}

	payload := parts[0]
	signature := parts[1]

	// Payload should contain sessionID and timestamp
	payloadParts := strings.Split(payload, "|")
	if len(payloadParts) != 2 {
		t.Errorf("Expected 2 payload parts separated by '|', got %d parts", len(payloadParts))
	}

	if payloadParts[0] != sessionID {
		t.Errorf("Expected session ID %s in payload, got %s", sessionID, payloadParts[0])
	}

	// Signature should be base64 encoded
	if signature == "" {
		t.Error("Expected non-empty signature")
	}
}

func TestVerifyTamperedSessionID(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create session signer: %v", err)
	}

	sessionID := "kitemcp-550e8400-e29b-41d4-a716-446655440000"
	signed := signer.SignSessionID(sessionID)

	// Tamper with the signed parameter
	tamperedSigned := strings.Replace(signed, sessionID, "kitemcp-different-session-id", 1)

	_, err = signer.VerifySessionID(tamperedSigned)
	if err != ErrTamperedSession {
		t.Errorf("Expected ErrTamperedSession, got %v", err)
	}
}

func TestVerifyInvalidFormat(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create session signer: %v", err)
	}

	testCases := []struct {
		name          string
		signedParam   string
		expectedError error
	}{
		{
			name:          "no dot separator",
			signedParam:   "sessionid-timestamp-signature",
			expectedError: ErrInvalidFormat,
		},
		{
			name:          "multiple dots",
			signedParam:   "session.id.timestamp.signature",
			expectedError: ErrInvalidFormat,
		},
		{
			name:          "invalid base64 signature",
			signedParam:   "sessionid|timestamp.invalid-base64!",
			expectedError: ErrInvalidSignature,
		},
		{
			name:          "no pipe in payload",
			signedParam:   "sessionid-timestamp.dGVzdA==",
			expectedError: ErrTamperedSession,
		},
		{
			name:          "invalid timestamp",
			signedParam:   "sessionid|notanumber.dGVzdA==",
			expectedError: ErrTamperedSession,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := signer.VerifySessionID(tc.signedParam)
			if err == nil {
				t.Error("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.expectedError.Error()) {
				t.Errorf("Expected error containing %v, got %v", tc.expectedError, err)
			}
		})
	}
}

func TestSignatureExpiry(t *testing.T) {
	synctest.Run(func() {
		signer, err := NewSessionSigner()
		if err != nil {
			t.Fatalf("Failed to create session signer: %v", err)
		}

		// Set a very short expiry for testing
		signer.SetSignatureExpiry(100 * time.Millisecond)

		sessionID := "kitemcp-550e8400-e29b-41d4-a716-446655440000"
		signed := signer.SignSessionID(sessionID)

		// Advance time beyond expiry + MaxClockSkew
		time.Sleep(100*time.Millisecond + MaxClockSkew + time.Second)

		_, err = signer.VerifySessionID(signed)
		if err != ErrExpiredSignature {
			t.Errorf("Expected ErrExpiredSignature, got %v", err)
		}
	})
}

func TestDifferentSigners(t *testing.T) {
	// Create two signers with different keys
	signer1, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create first signer: %v", err)
	}

	signer2, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create second signer: %v", err)
	}

	sessionID := "kitemcp-550e8400-e29b-41d4-a716-446655440000"

	// Sign with signer1
	signed := signer1.SignSessionID(sessionID)

	// Try to verify with signer2 (should fail)
	_, err = signer2.VerifySessionID(signed)
	if err != ErrTamperedSession {
		t.Errorf("Expected ErrTamperedSession when using different signers, got %v", err)
	}
}

func TestSignRedirectParams(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create session signer: %v", err)
	}

	sessionID := "kitemcp-550e8400-e29b-41d4-a716-446655440000"

	redirectParams, err := signer.SignRedirectParams(sessionID)
	if err != nil {
		t.Fatalf("Failed to sign redirect params: %v", err)
	}

	// Should start with "session_id="
	if !strings.HasPrefix(redirectParams, "session_id=") {
		t.Errorf("Expected redirect params to start with 'session_id=', got %s", redirectParams)
	}

	// Verify the redirect params
	verifiedSessionID, err := signer.VerifyRedirectParams(redirectParams)
	if err != nil {
		t.Fatalf("Failed to verify redirect params: %v", err)
	}

	if verifiedSessionID != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, verifiedSessionID)
	}
}

func TestSignRedirectParamsInvalidSessionID(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create session signer: %v", err)
	}

	invalidSessionID := "invalid-session-id"

	_, err = signer.SignRedirectParams(invalidSessionID)
	if err == nil {
		t.Error("Expected error for invalid session ID")
	}
}

func TestVerifyRedirectParamsInvalidFormat(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create session signer: %v", err)
	}

	testCases := []string{
		"invalid=params",
		"session_id=",
		"different_param=value",
		"",
	}

	for _, tc := range testCases {
		t.Run(tc, func(t *testing.T) {
			_, err := signer.VerifyRedirectParams(tc)
			if err != ErrInvalidFormat {
				t.Errorf("Expected ErrInvalidFormat, got %v", err)
			}
		})
	}
}

func TestGetSecretKey(t *testing.T) {
	secretKey := []byte("test-secret-key-32-bytes-long!!")
	signer := NewSessionSignerWithKey(secretKey)

	retrievedKey := signer.GetSecretKey()

	// Should return a copy
	if &retrievedKey[0] == &signer.secretKey[0] {
		t.Error("GetSecretKey should return a copy, not the original slice")
	}

	// But content should be the same
	if string(retrievedKey) != string(secretKey) {
		t.Error("GetSecretKey should return the same content")
	}

	// Modifying the returned key shouldn't affect the original
	retrievedKey[0] = 'X'
	if signer.secretKey[0] == 'X' {
		t.Error("Modifying returned key affected the original")
	}
}

func TestValidateSessionID(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatalf("Failed to create session signer: %v", err)
	}

	testCases := []struct {
		name      string
		sessionID string
		expectErr bool
	}{
		{
			name:      "valid session ID",
			sessionID: "kitemcp-550e8400-e29b-41d4-a716-446655440000",
			expectErr: false,
		},
		{
			name:      "invalid prefix",
			sessionID: "invalid-550e8400-e29b-41d4-a716-446655440000",
			expectErr: true,
		},
		{
			name:      "invalid UUID",
			sessionID: "kitemcp-invalid-uuid",
			expectErr: true,
		},
		{
			name:      "empty session ID",
			sessionID: "",
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := signer.ValidateSessionID(tc.sessionID)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestClockSkewTolerance(t *testing.T) {
	synctest.Run(func() {
		signer, err := NewSessionSigner()
		if err != nil {
			t.Fatalf("Failed to create session signer: %v", err)
		}

		// Set expiry to 1 second for testing
		signer.SetSignatureExpiry(1 * time.Second)

		sessionID := "kitemcp-550e8400-e29b-41d4-a716-446655440000"
		signed := signer.SignSessionID(sessionID)

		// Wait past expiry but within clock skew tolerance
		time.Sleep(2 * time.Second) // Past 1s expiry but within 5min MaxClockSkew

		// Should still be valid due to MaxClockSkew tolerance
		_, err = signer.VerifySessionID(signed)
		if err != nil {
			t.Errorf("Expected signature to be valid within clock skew tolerance, got: %v", err)
		}

		// Create new signature and wait beyond clock skew tolerance
		signed2 := signer.SignSessionID(sessionID)
		time.Sleep(1*time.Second + MaxClockSkew + 1*time.Second) // Beyond expiry + MaxClockSkew

		_, err = signer.VerifySessionID(signed2)
		if err != ErrExpiredSignature {
			t.Errorf("Expected ErrExpiredSignature beyond clock skew, got %v", err)
		}
	})
}

func BenchmarkSignSessionID(b *testing.B) {
	signer, err := NewSessionSigner()
	if err != nil {
		b.Fatalf("Failed to create session signer: %v", err)
	}

	sessionID := "kitemcp-550e8400-e29b-41d4-a716-446655440000"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		signer.SignSessionID(sessionID)
	}
}

func BenchmarkVerifySessionID(b *testing.B) {
	signer, err := NewSessionSigner()
	if err != nil {
		b.Fatalf("Failed to create session signer: %v", err)
	}

	sessionID := "kitemcp-550e8400-e29b-41d4-a716-446655440000"
	signed := signer.SignSessionID(sessionID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		signer.VerifySessionID(signed)
	}
}
