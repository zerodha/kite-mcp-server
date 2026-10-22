package kc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// sessionTransport handles explicit upstream token rejection, including rejection
// before the estimated daily cutoff. Only GET requests may be replayed, once.
// Mutations are never replayed; their rejection invalidates the cached credential
// so a subsequent tool call can renew before making another upstream request.
type sessionTransport struct {
	manager     *Manager
	sessionID   string
	base        http.RoundTripper
	callContext context.Context
}

func (t *sessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.callContext == nil {
		return t.roundTrip(req)
	}
	// Keep net/http's timeout context as well as the tool's cancellation. Retain
	// both until the response body is closed, not merely until headers arrive.
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(t.callContext, cancel)
	cleanup := func() { stop(); cancel() }
	if err := t.callContext.Err(); err != nil {
		cleanup()
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	response, err := t.roundTrip(req.Clone(ctx))
	if err != nil {
		cleanup()
		return response, err
	}
	if response.Body == nil {
		cleanup()
	} else {
		response.Body = &contextResponseBody{ReadCloser: response.Body, cleanup: cleanup}
	}
	return response, nil
}

type contextResponseBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *contextResponseBody) Close() error {
	defer b.cleanup()
	return b.ReadCloser.Close()
}

func (t *sessionTransport) roundTrip(req *http.Request) (*http.Response, error) {
	if err := t.requestContextError(req); err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	// Check again at send time: this client may have been retained across a
	// renewal, logout, or grant expiry.
	creds, err := t.manager.authenticatedCredentials(t.sessionID)
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	if err := t.requestContextError(req); err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	attempt := req.Clone(req.Context())
	attempt.Header.Set("Authorization", "token "+t.manager.apiKey+":"+creds.AccessToken)
	rejectedToken := creds.AccessToken
	rejectedGeneration := creds.generation
	response, err := t.base.RoundTrip(attempt)
	if err != nil || !tokenRejected(response) {
		return response, err
	}
	t.manager.sessionManager.rejectAccessToken(t.sessionID, rejectedToken, rejectedGeneration)
	if req.Method != http.MethodGet {
		return response, nil
	}
	creds, err = t.manager.authenticatedCredentials(t.sessionID)
	if err != nil {
		_ = response.Body.Close()
		return nil, err
	}
	if err := t.requestContextError(req); err != nil {
		_ = response.Body.Close()
		return nil, err
	}
	retry := req.Clone(req.Context())
	retry.Header.Set("Authorization", "token "+t.manager.apiKey+":"+creds.AccessToken)
	_ = response.Body.Close()
	response, err = t.base.RoundTrip(retry)
	if err == nil && tokenRejected(response) {
		t.manager.sessionManager.rejectAccessToken(t.sessionID, creds.AccessToken, creds.generation)
	}
	return response, err
}

// Check the tool context synchronously too: AfterFunc's cancellation callback
// may not have run yet when a credential renewal unblocks.
func (t *sessionTransport) requestContextError(req *http.Request) error {
	if t.callContext != nil {
		if err := t.callContext.Err(); err != nil {
			return err
		}
	}
	return req.Context().Err()
}

// Inspect a bounded error body and restore it for the Kite SDK to parse. A 403
// PermissionError is not an authentication failure and must not trigger refresh.
func tokenRejected(response *http.Response) bool {
	if response == nil || (response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden) {
		return false
	}
	const maxErrorBytes = 64 << 10
	prefix, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBytes+1))
	response.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(prefix), response.Body), response.Body}
	if err != nil || len(prefix) > maxErrorBytes {
		return false
	}
	var envelope struct {
		ErrorType string `json:"error_type"`
	}
	return json.Unmarshal(prefix, &envelope) == nil && envelope.ErrorType == "TokenException"
}

// Never follow redirects with account credentials or replay a mutation through
// a 307/308 response. This also keeps our authorization wrapper on Kite's origin.
func newKiteHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Timeout:       7 * time.Second,
		Transport:     transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}
