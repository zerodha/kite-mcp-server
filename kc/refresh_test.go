package kc

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func refreshTestManager(t *testing.T, handler http.HandlerFunc) *Manager {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	manager, err := newTestManager("test_key", "test_secret")
	if err != nil {
		t.Fatal(err)
	}
	manager.kiteBaseURI = server.URL
	t.Cleanup(manager.Shutdown)
	return manager
}

func expiredCredentialSession(t *testing.T, manager *Manager, refresh string) string {
	t.Helper()
	id := manager.sessionManager.Generate()
	err := manager.sessionManager.UpdateCredentials(id, &KiteCredentials{
		AccessToken: "old-access", RefreshToken: refresh, UserID: "user", ExpiresAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestKiteAccessTokenExpiry(t *testing.T) {
	for _, tc := range []struct{ now, want string }{
		{"2026-10-22T05:59:59+05:30", "2026-10-22T06:00:00+05:30"},
		{"2026-10-22T06:00:00+05:30", "2026-10-23T06:00:00+05:30"},
		{"2026-12-31T23:00:00+05:30", "2027-01-01T06:00:00+05:30"},
		{"2026-10-22T01:00:00Z", "2026-10-23T06:00:00+05:30"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		want, _ := time.Parse(time.RFC3339, tc.want)
		if got := kiteAccessTokenExpiry(now); !got.Equal(want) {
			t.Errorf("%s: got %s, want %s", tc.now, got, want)
		}
	}
}

func TestCompleteLoginRetainsRefreshToken(t *testing.T) {
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/token" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"user_id":"user","access_token":"access","refresh_token":"refresh"}}`)
	})
	before := time.Now()
	creds, err := manager.CompleteLogin("request")
	if err != nil {
		t.Fatal(err)
	}
	if creds.RefreshToken != "refresh" || creds.AccessToken != "access" || creds.UserID != "user" {
		t.Fatalf("login credentials not retained")
	}
	if !creds.ExpiresAt.Equal(kiteAccessTokenExpiry(before)) {
		t.Fatal("incorrect access expiry")
	}
}

func TestConcurrentRefreshPreservesGrant(t *testing.T) {
	for _, rotated := range []string{"rotated", ""} {
		t.Run("rotation="+rotated, func(t *testing.T) {
			var calls atomic.Int32
			manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/session/refresh_token" {
					t.Errorf("unexpected renewal request")
				}
				_ = r.ParseForm()
				checksum := fmt.Sprintf("%x", sha256.Sum256([]byte("test_keyrefreshtest_secret")))
				if r.Form.Get("refresh_token") != "refresh" || r.Form.Get("checksum") != checksum {
					t.Errorf("bad renewal credentials/checksum")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"status":"success","data":{"user_id":"user","access_token":"new-access","refresh_token":%q}}`, rotated)
			})
			id := expiredCredentialSession(t, manager, "refresh")
			before, _ := manager.sessionManager.Get(id)
			var wg sync.WaitGroup
			for range 16 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					client, err := manager.GetAuthenticatedClient(id)
					if err != nil || client == nil {
						t.Errorf("refresh failed: %v", err)
					}
				}()
			}
			wg.Wait()
			after, _ := manager.sessionManager.Get(id)
			if calls.Load() != 1 {
				t.Fatalf("got %d renewals, want one", calls.Load())
			}
			wantRefresh := rotated
			if wantRefresh == "" {
				wantRefresh = "refresh"
			}
			if after.Credentials.AccessToken != "new-access" || after.Credentials.RefreshToken != wantRefresh {
				t.Fatal("renewed credentials not saved")
			}
			if !after.ExpiresAt.Equal(before.ExpiresAt) {
				t.Fatal("refresh extended fixed grant")
			}
		})
	}
}

func TestRefreshFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
		revoked          bool
	}{
		{"revoked", `{"status":"error","error_type":"TokenException","message":"revoked"}`, "re-authenticate", 403, true},
		{"temporary", `{"status":"error","error_type":"NetworkException","message":"unavailable"}`, "try again later", 503, false},
		{"empty-token", `{"status":"success","data":{}}`, "invalid Kite token renewal response", 200, false},
		{"wrong-user", `{"status":"success","data":{"user_id":"other","access_token":"new"}}`, "invalid Kite token renewal response", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			})
			id := expiredCredentialSession(t, manager, "refresh")
			_, err := manager.GetAuthenticatedClient(id)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: %v", err)
			}
			session, getErr := manager.sessionManager.Get(id)
			if tc.revoked {
				if getErr == nil {
					t.Fatal("rejected grant retained")
				}
			} else if getErr != nil || session.Credentials.RefreshToken != "refresh" || session.Credentials.AccessToken != "old-access" {
				t.Fatal("failed refresh changed existing credentials")
			}
		})
	}
}

func TestLogoutDuringRefreshDoesNotRestoreGrant(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"access_token":"new-access"}}`)
	})
	id := expiredCredentialSession(t, manager, "refresh")
	done := make(chan error, 1)
	go func() { _, err := manager.GetAuthenticatedClient(id); done <- err }()
	<-started
	_, err := manager.sessionManager.Terminate(id)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("renewal succeeded after logout")
	}
	if _, err := manager.sessionManager.Get(id); err == nil {
		t.Fatal("renewal restored logged-out session")
	}
}

func TestNoRefreshForExpiredGrantOrMissingRefreshToken(t *testing.T) {
	var calls atomic.Int32
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
	for _, expiredGrant := range []bool{false, true} {
		refresh := ""
		if expiredGrant {
			refresh = "refresh"
		}
		id := expiredCredentialSession(t, manager, refresh)
		if expiredGrant {
			manager.sessionManager.mu.Lock()
			manager.sessionManager.sessions[id].ExpiresAt = time.Now().Add(-time.Second)
			manager.sessionManager.mu.Unlock()
		}
		if _, err := manager.GetAuthenticatedClient(id); err == nil {
			t.Fatal("expected reauthentication")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("attempted refresh without valid grant and refresh token")
	}
}

func TestFixedSevenDayGrantExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewSessionManager(testLogger())
		id := manager.Generate()
		session, _ := manager.Get(id)
		if session.ExpiresAt.Sub(session.CreatedAt) != 7*24*time.Hour {
			t.Fatal("grant is not seven days")
		}
		time.Sleep(6 * 24 * time.Hour)
		creds := &KiteCredentials{AccessToken: "renewed", RefreshToken: "refresh"}
		if err := manager.UpdateCredentials(id, creds); err != nil {
			t.Fatal(err)
		}
		after, _ := manager.Get(id)
		if !after.ExpiresAt.Equal(session.ExpiresAt) {
			t.Fatal("renewal extended grant")
		}
		time.Sleep(24 * time.Hour)
		if err := manager.UpdateCredentials(id, creds); err == nil {
			t.Fatal("updated expired grant")
		}
		if manager.CleanupExpiredSessions() != 1 {
			t.Fatal("expired grant not cleaned up")
		}
	})
}

func TestPendingAuthorizationIsShortLived(t *testing.T) {
	manager := NewSessionManager(testLogger())
	id := manager.GenerateWithData("pending-authorization")
	session, err := manager.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if session.ExpiresAt.Sub(session.CreatedAt) != 10*time.Minute {
		t.Fatal("pending authorization inherited the seven-day grant lifetime")
	}
}

func TestEarlyTokenRejectionRenewsSafeRead(t *testing.T) {
	var renewals, reads atomic.Int32
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/session/refresh_token" {
			renewals.Add(1)
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"user_id":"user","access_token":"new-access","refresh_token":"rotated"}}`)
			return
		}
		reads.Add(1)
		if r.Header.Get("Authorization") == "token test_key:old-access" {
			w.WriteHeader(403)
			_, _ = fmt.Fprint(w, `{"status":"error","error_type":"TokenException","message":"rejected"}`)
			return
		}
		if r.Header.Get("Authorization") != "token test_key:new-access" {
			t.Error("read used unexpected token")
		}
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"user_id":"user"}}`)
	})
	id := expiredCredentialSession(t, manager, "refresh")
	// Access is locally valid but upstream has already invalidated it.
	session, _ := manager.sessionManager.Get(id)
	session.Credentials.ExpiresAt = time.Now().Add(time.Hour)
	if err := manager.sessionManager.UpdateCredentials(id, session.Credentials); err != nil {
		t.Fatal(err)
	}
	client, err := manager.GetAuthenticatedClient(id)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		profile, err := client.GetUserProfile()
		if err != nil || profile.UserID != "user" {
			t.Fatalf("read failed after refresh: %v", err)
		}
	}
	if renewals.Load() != 1 || reads.Load() != 3 {
		t.Fatalf("renewals=%d reads=%d", renewals.Load(), reads.Load())
	}
}

func TestTokenRejectionNeverReplaysMutation(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var mutations, renewals atomic.Int32
			manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/session/refresh_token" {
					renewals.Add(1)
					_, _ = fmt.Fprint(w, `{"status":"success","data":{"user_id":"user","access_token":"new"}}`)
					return
				}
				mutations.Add(1)
				w.WriteHeader(403)
				_, _ = fmt.Fprint(w, `{"status":"error","error_type":"TokenException","message":"rejected"}`)
			})
			id := expiredCredentialSession(t, manager, "refresh")
			session, _ := manager.sessionManager.Get(id)
			session.Credentials.ExpiresAt = time.Now().Add(time.Hour)
			if err := manager.sessionManager.UpdateCredentials(id, session.Credentials); err != nil {
				t.Fatal(err)
			}
			transport := &sessionTransport{manager: manager, sessionID: id, base: http.DefaultTransport}
			req, err := http.NewRequest(method, manager.kiteBaseURI+"/orders", strings.NewReader("quantity=1"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != 403 || mutations.Load() != 1 || renewals.Load() != 0 {
				t.Fatal("mutation replayed or renewed during ambiguous operation")
			}
			if _, err := manager.GetAuthenticatedClient(id); err != nil {
				t.Fatal(err)
			}
			if renewals.Load() != 1 {
				t.Fatal("next call did not renew rejected credential")
			}
		})
	}
}

func TestConcurrentRefreshFailureBackoff(t *testing.T) {
	var renewals atomic.Int32
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		renewals.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if renewals.Load() == 1 {
			w.WriteHeader(503)
			_, _ = fmt.Fprint(w, `{"status":"error","error_type":"NetworkException","message":"temporary"}`)
		} else {
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"access_token":"new","refresh_token":"rotated"}}`)
		}
	})
	id := expiredCredentialSession(t, manager, "refresh")
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := manager.GetAuthenticatedClient(id); err == nil {
				t.Error("expected shared transient failure")
			}
		}()
	}
	wg.Wait()
	if renewals.Load() != 1 {
		t.Fatalf("got %d renewals during cooldown", renewals.Load())
	}
	// Advance the stored retry deadline without slowing the test.
	manager.sessionManager.mu.Lock()
	manager.sessionManager.sessions[id].refreshRetryAt = time.Now().Add(-time.Second)
	manager.sessionManager.mu.Unlock()
	if _, err := manager.GetAuthenticatedClient(id); err != nil {
		t.Fatal(err)
	}
	if renewals.Load() != 2 {
		t.Fatal("did not retry after cooldown")
	}
	session, _ := manager.sessionManager.Get(id)
	if session.refreshFailure != nil || !session.refreshRetryAt.IsZero() {
		t.Fatal("successful refresh retained backoff")
	}
}

func TestPermissionErrorDoesNotRenew(t *testing.T) {
	var calls atomic.Int32
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = fmt.Fprint(w, `{"status":"error","error_type":"PermissionError","message":"not allowed"}`)
	})
	id := expiredCredentialSession(t, manager, "")
	session, _ := manager.sessionManager.Get(id)
	session.Credentials.ExpiresAt = time.Now().Add(time.Hour)
	if err := manager.sessionManager.UpdateCredentials(id, session.Credentials); err != nil {
		t.Fatal(err)
	}
	client, err := manager.GetAuthenticatedClient(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetUserProfile(); err == nil {
		t.Fatal("expected permission error")
	}
	if calls.Load() != 1 {
		t.Fatal("permission failure was replayed")
	}
}

func TestConcurrentEarlyRejectionsWithUnchangedAccessToken(t *testing.T) {
	var reads, renewals atomic.Int32
	bothStarted := make(chan struct{})
	// Kite may keep the access token unchanged while rotating refresh tokens.
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/session/refresh_token" {
			renewals.Add(1)
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"user_id":"user","access_token":"same-access","refresh_token":"rotated"}}`)
			return
		}
		count := reads.Add(1)
		if count <= 2 {
			if count == 2 {
				close(bothStarted)
			}
			<-bothStarted
			w.WriteHeader(403)
			_, _ = fmt.Fprint(w, `{"status":"error","error_type":"TokenException","message":"rejected"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"user_id":"user"}}`)
	})
	id := manager.sessionManager.Generate()
	if err := manager.sessionManager.UpdateCredentials(id, &KiteCredentials{UserID: "user", AccessToken: "same-access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	client, err := manager.GetAuthenticatedClient(id)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.GetUserProfile(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if renewals.Load() != 1 || reads.Load() != 4 {
		t.Fatalf("renewals=%d reads=%d; stale rejection invalidated rotated credentials", renewals.Load(), reads.Load())
	}
	session, _ := manager.sessionManager.Get(id)
	if session.Credentials.RefreshToken != "rotated" || !time.Now().Before(session.Credentials.ExpiresAt) {
		t.Fatal("fresh credentials invalidated by late rejection")
	}
}

func TestRetainedClientCannotSendAfterLogout(t *testing.T) {
	var calls atomic.Int32
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
	id := manager.sessionManager.Generate()
	if err := manager.sessionManager.UpdateCredentials(id, &KiteCredentials{UserID: "user", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	client, err := manager.GetAuthenticatedClient(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.sessionManager.Terminate(id); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetUserProfile(); err == nil {
		t.Fatal("retained client sent a request after logout")
	}
	if calls.Load() != 0 {
		t.Fatal("upstream called after logout")
	}
}

func TestAuthenticatedClientDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	id := manager.sessionManager.Generate()
	if err := manager.sessionManager.UpdateCredentials(id, &KiteCredentials{UserID: "user", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	client, err := manager.GetAuthenticatedClient(id)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.GetUserProfile()
	if redirected.Load() != 0 {
		t.Fatal("account credentials followed upstream redirect")
	}
}
