package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPOriginValidationAllMethods(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions} {
		for _, origin := range []string{"https://evil.example", "null", "", "https://mcp.example/path", "https://user@mcp.example", "http://mcp.example", "https://mcp.example?", "https://mcp.example#", "https://mcp.example#fragment", "https://mcp.example https://evil.example"} {
			t.Run(method+"/"+origin, func(t *testing.T) {
				reached := false
				h := validateMCPOrigin("https://mcp.example", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
				r := httptest.NewRequest(method, "http://internal/mcp", nil)
				r.Header["Origin"] = []string{origin}
				r.Header.Set("Sec-Fetch-Site", "same-origin") // Cannot override an invalid Origin.
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden || reached {
					t.Fatalf("invalid origin reached handler: status=%d reached=%v", w.Code, reached)
				}
			})
		}
		for _, origin := range []string{"https://mcp.example", "absent"} {
			t.Run(method+"/allowed/"+origin, func(t *testing.T) {
				reached := false
				h := validateMCPOrigin("https://mcp.example", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; w.WriteHeader(http.StatusNoContent) }))
				r := httptest.NewRequest(method, "http://internal/mcp", nil)
				if origin != "absent" {
					r.Header.Set("Origin", origin)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != http.StatusNoContent || !reached {
					t.Fatalf("allowed origin rejected: %d", w.Code)
				}
			})
		}
	}
}

func TestMCPOriginPreservesSDKOptionsHandling(t *testing.T) {
	sdk := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return nil }, nil)
	for _, origin := range []string{"https://mcp.example", "absent"} {
		r := httptest.NewRequest(http.MethodOptions, "http://internal/mcp", nil)
		if origin != "absent" {
			r.Header.Set("Origin", origin)
		}
		want := httptest.NewRecorder()
		sdk.ServeHTTP(want, r.Clone(r.Context()))
		got := httptest.NewRecorder()
		validateMCPOrigin("https://mcp.example", sdk).ServeHTTP(got, r)
		if got.Code != want.Code || got.Header().Get("Allow") != want.Header().Get("Allow") {
			t.Fatalf("OPTIONS changed: got %d/%q want %d/%q", got.Code, got.Header().Get("Allow"), want.Code, want.Header().Get("Allow"))
		}
	}
}

func TestMCPOriginEffectivePorts(t *testing.T) {
	for _, tc := range []struct {
		public, origin string
		allowed        bool
	}{
		{"https://mcp.example", "https://mcp.example:443", true},
		{"https://mcp.example:443", "https://mcp.example", true},
		{"http://localhost", "http://localhost:80", true},
		{"http://localhost:80", "http://localhost", true},
		{"https://MCP.example:443", "https://mcp.EXAMPLE", true},
		{"https://mcp.example:8443", "https://mcp.example:8443", true},
		{"https://[::1]", "https://[::1]:443", true},
		{"https://mcp.example", "https://mcp.example:8443", false},
		{"https://mcp.example:8443", "https://mcp.example", false},
		{"https://mcp.example:8443", "https://mcp.example:9443", false},
		{"http://mcp.example", "http://mcp.example:443", false},
		{"https://mcp.example", "http://mcp.example:443", false},
		{"https://mcp.example", "https://evil.example:443", false},
		{"https://mcp.example", "https://mcp.example:", false},
		{"https://mcp.example", "https://mcp.example:99999", false},
	} {
		t.Run(tc.public+"/"+tc.origin, func(t *testing.T) {
			h := validateMCPOrigin(tc.public, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			r := httptest.NewRequest(http.MethodGet, "http://internal/mcp", nil)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := http.StatusForbidden
			if tc.allowed {
				want = http.StatusNoContent
			}
			if w.Code != want {
				t.Fatalf("got %d, want %d", w.Code, want)
			}
		})
	}
}

func TestMCPOriginRejectsDuplicateHeaders(t *testing.T) {
	for _, origins := range [][]string{
		{"https://mcp.example", "https://mcp.example:443"},
		{"https://mcp.example", "https://evil.example"},
	} {
		h := validateMCPOrigin("https://mcp.example", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("duplicate origins reached handler") }))
		r := httptest.NewRequest(http.MethodGet, "http://internal/mcp", nil)
		r.Header["Origin"] = origins
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("duplicate origins returned %d", w.Code)
		}
	}
}
