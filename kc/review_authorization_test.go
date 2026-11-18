package kc

import (
	"bytes"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestSealedReviewAuthorizationRejectsTamperingAndWrongSigner(t *testing.T) {
	signer, err := NewSessionSigner()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"account":"private-account","expires":"fixed-deadline"}`)
	first, err := signer.SealReviewAuthorization(payload)
	if err != nil {
		t.Fatal(err)
	}
	second, err := signer.SealReviewAuthorization(payload)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || strings.Contains(first, "private-account") {
		t.Fatal("state is not independently sealed")
	}
	decoded, err := signer.OpenReviewAuthorization(first)
	if err != nil || !bytes.Equal(payload, decoded) {
		t.Fatal("sealed state did not round-trip", err)
	}
	other, err := NewSessionSigner()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.OpenReviewAuthorization(first); err == nil {
		t.Fatal("another signer accepted the token")
	}
	position := len(ReviewAuthorizationPrefix) + 3
	changed := byte('A')
	if first[position] == changed {
		changed = 'B'
	}
	if _, err := signer.OpenReviewAuthorization(first[:position] + string(changed) + first[position+1:]); err == nil {
		t.Fatal("tampered token accepted")
	}
	if _, err := signer.OpenReviewAuthorization(ReviewAuthorizationPrefix + "bad"); err == nil {
		t.Fatal("truncated token accepted")
	}
	if _, err := signer.SealReviewAuthorization(make([]byte, maxReviewAuthorizationBytes+1)); err == nil {
		t.Fatal("oversized state accepted")
	}
}

func TestStatelessAuthorizationLookupDoesNotAdmitSessionsAndExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sm := NewSessionManager(testLogger())
		deadline := time.Now().Add(5 * time.Minute)
		if err := sm.RegisterStatelessAuthorization("sealed-", func(id string) (any, time.Time, error) { return id, deadline, nil }); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 100; i++ {
			if _, err := sm.Get("sealed-test"); err != nil {
				t.Fatal(err)
			}
		}
		if sm.sessions.Len() != 0 || sm.pendingAuth.Len() != 0 || sm.pendingReviews.Len() != 0 {
			t.Fatal("stateless reads admitted state")
		}
		time.Sleep(5*time.Minute + time.Second)
		if _, err := sm.Get("sealed-test"); err == nil {
			t.Fatal("expired stateless state accepted")
		}
	})
}
