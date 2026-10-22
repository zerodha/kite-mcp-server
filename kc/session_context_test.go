package kc

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

func TestOrderContextCancellationDuringCredentialRenewal(t *testing.T) {
	renewing, release := make(chan struct{}), make(chan struct{})
	var mutations atomic.Int32
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session/refresh_token" {
			close(renewing)
			<-release
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"success","data":{"user_id":"user","access_token":"new-access","refresh_token":"refresh"}}`)
			return
		}
		mutations.Add(1)
		_, _ = io.WriteString(w, `{"status":"success","data":{"order_id":"unexpected"}}`)
	})
	id := manager.sessionManager.Generate()
	require.NoError(t, manager.sessionManager.UpdateCredentials(id, &KiteCredentials{AccessToken: "access", RefreshToken: "refresh", UserID: "user", ExpiresAt: time.Now().Add(time.Hour)}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := manager.GetAuthenticatedClientContext(ctx, id)
	require.NoError(t, err)
	require.NoError(t, manager.sessionManager.UpdateCredentials(id, &KiteCredentials{AccessToken: "access", RefreshToken: "refresh", UserID: "user", ExpiresAt: time.Now().Add(-time.Hour)}))
	done := make(chan error, 1)
	go func() {
		_, err := client.PlaceOrder("regular", kiteconnect.OrderParams{Exchange: "NSE", Tradingsymbol: "INFY", TransactionType: "BUY", Quantity: 1, Product: "CNC", OrderType: "MARKET"})
		done <- err
	}()
	select {
	case <-renewing:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("renewal did not start")
	}
	cancel()
	close(release)
	select {
	case err := <-done:
		require.Error(t, err) // gokiteconnect translates context errors into NetworkException.
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled mutation did not return")
	}
	require.Zero(t, mutations.Load(), "cancellation during renewal must prevent the order request")
}

func TestOrderHTTPContextCancellation(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	manager := refreshTestManager(t, func(w http.ResponseWriter, r *http.Request) {
		// Drain the request body so the server can observe connection cancellation.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(stopped)
	})
	id := manager.sessionManager.Generate()
	require.NoError(t, manager.sessionManager.UpdateCredentials(id, &KiteCredentials{AccessToken: "access", UserID: "user", ExpiresAt: time.Now().Add(time.Hour)}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := manager.GetAuthenticatedClientContext(ctx, id)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := client.PlaceOrder("regular", kiteconnect.OrderParams{Quantity: 1}); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.Error(t, err) // gokiteconnect does not preserve the cancellation error chain.
	case <-time.After(3 * time.Second):
		t.Fatal("request ignored cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Kite request context was not cancelled")
	}
}
