package kc

import (
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
	base   http.RoundTripper
	status atomic.Int64
}

func (t *loginResponseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if response != nil {
		t.status.Store(int64(response.StatusCode))
	}
	return response, err
}
