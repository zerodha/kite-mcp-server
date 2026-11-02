package kc

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
)

// LoginError keeps the observed HTTP status separate from synthetic SDK codes.
// UpstreamStatus is zero when no HTTP response was received.
type LoginError struct {
	UpstreamStatus int
	Err            error
}

func (e *LoginError) Error() string { return fmt.Sprintf("failed to generate Kite session: %v", e.Err) }
func (e *LoginError) Unwrap() error { return e.Err }

// Each exchange owns its transport, so concurrent logins cannot mix statuses.
type loginResponseTransport struct {
	ctx    context.Context
	base   http.RoundTripper
	status atomic.Int64
}

func (t *loginResponseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.ctx == nil {
		response, err := t.base.RoundTrip(r)
		if response != nil {
			t.status.Store(int64(response.StatusCode))
		}
		return response, err
	}
	// Preserve the HTTP client's shorter timeout as well as the review's
	// cancellation/deadline, including while the response body is read.
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	if err := t.ctx.Err(); err != nil {
		cleanup()
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, err
	}
	response, err := t.base.RoundTrip(r.Clone(ctx))
	if response != nil {
		t.status.Store(int64(response.StatusCode))
	}
	if err != nil || response == nil || response.Body == nil {
		cleanup()
	} else {
		response.Body = &contextResponseBody{ReadCloser: response.Body, cleanup: cleanup}
	}
	return response, err
}
