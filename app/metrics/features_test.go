package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPMetricsPreserveResponseAndDoNotExposeRequest(t *testing.T) {
	m := New(Config{})
	defer m.Shutdown()
	handler := m.HTTPMiddleware("oauth_http", "register", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test", "present")
		w.WriteHeader(http.StatusTooManyRequests)
		w.WriteHeader(http.StatusOK) // The first response status is authoritative.
		_, err := w.Write([]byte("rate limited"))
		require.NoError(t, err)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/private-path?client_secret=private-value", nil))
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Equal(t, "present", w.Header().Get("X-Test"))
	require.Equal(t, "rate limited", w.Body.String())
	scrape := httptest.NewRecorder()
	m.HTTPHandler()(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Contains(t, scrape.Body.String(), `kite_mcp_operations_total{mode="register",outcome="client_error",service="kite-mcp",tool="oauth_http"} 1`)
	require.Contains(t, scrape.Body.String(), `kite_mcp_operation_duration_seconds_count{mode="register",service="kite-mcp",tool="oauth_http"} 1`)
	require.NotContains(t, scrape.Body.String(), "private-")
	var disabled *Manager
	disabled.HTTPMiddleware("oauth_http", "register", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestHTTPMetricsPreserveInformationalAndFlushedResponses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flush   bool
		want    int
		outcome string
	}{
		{"early hints", false, http.StatusTooManyRequests, "client_error"},
		{"flush commits success", true, http.StatusOK, "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Config{})
			defer m.Shutdown()
			server := httptest.NewServer(m.HTTPMiddleware("oauth_http", "register", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.flush {
					require.NoError(t, http.NewResponseController(w).Flush())
				} else {
					w.WriteHeader(http.StatusEarlyHints)
				}
				w.WriteHeader(http.StatusTooManyRequests)
				_, err := w.Write([]byte("body preserved"))
				require.NoError(t, err)
			})))
			defer server.Close()
			response, err := server.Client().Get(server.URL)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, tc.want, response.StatusCode)
			require.Equal(t, "body preserved", string(body))
			scrape := httptest.NewRecorder()
			m.HTTPHandler()(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			require.Contains(t, scrape.Body.String(), `kite_mcp_operations_total{mode="register",outcome="`+tc.outcome+`",service="kite-mcp",tool="oauth_http"} 1`)
		})
	}
}

// Hide Flush and Unwrap to model an ordinary non-streaming response wrapper.
type noFlushResponse struct{ http.ResponseWriter }

func TestUnsupportedFlushDoesNotCommitSuccess(t *testing.T) {
	m := New(Config{})
	defer m.Shutdown()
	handler := m.HTTPMiddleware("oauth_http", "register", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.ErrorIs(t, http.NewResponseController(w).Flush(), http.ErrNotSupported)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(noFlushResponse{response}, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}
