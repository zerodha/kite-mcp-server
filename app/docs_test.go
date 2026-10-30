package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDocsHandlerServesIndex(t *testing.T) {
	h := newDocsHandler(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>home</h1>")},
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "home") {
		t.Fatalf("expected index body, got %q", rr.Body.String())
	}
}

func TestDocsHandlerServesDirectoryIndex(t *testing.T) {
	h := newDocsHandler(fstest.MapFS{
		"guide/index.html": &fstest.MapFile{Data: []byte("guide page")},
	})

	for _, url := range []string{"/guide", "/guide/"} {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", url, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "guide page") {
			t.Fatalf("%s: expected guide page, got %q", url, rr.Body.String())
		}
	}
}

func TestDocsHandlerReturns404InsteadOfDirectoryListing(t *testing.T) {
	h := newDocsHandler(fstest.MapFS{
		".gitkeep": &fstest.MapFile{Data: []byte{}},
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d with body %q", rr.Code, rr.Body.String())
	}
}

func TestDocsHandlerBlocksDotfiles(t *testing.T) {
	h := newDocsHandler(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>home</h1>")},
		".gitkeep":   &fstest.MapFile{Data: []byte("secret")},
	})

	req := httptest.NewRequest(http.MethodGet, "/.gitkeep", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestDocsNotFoundPage(t *testing.T) {
	handler := newDocsHandler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("home")}})
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, "/missing?request_token=secret", nil))
		if w.Code != 404 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("incorrect response: %d %v", w.Code, w.Header())
		}
		for _, want := range []string{"<!doctype html>", "Page not found", `href="/"`, "Go to documentation"} {
			if !strings.Contains(w.Body.String(), want) {
				t.Fatalf("missing %q", want)
			}
		}
		for _, unwanted := range []string{"secret", "request_token", "MCP client"} {
			if strings.Contains(w.Body.String(), unwanted) {
				t.Fatalf("unexpected %q", unwanted)
			}
		}
	}
}

type failedDocsWriter struct {
	header   http.Header
	statuses []int
	writes   int
}

func (w *failedDocsWriter) Header() http.Header    { return w.header }
func (w *failedDocsWriter) WriteHeader(status int) { w.statuses = append(w.statuses, status) }
func (w *failedDocsWriter) Write(p []byte) (int, error) {
	w.writes++
	return min(16, len(p)), io.ErrUnexpectedEOF
}
func TestDocsWriteFailureDoesNotAppendFallback(t *testing.T) {
	w := &failedDocsWriter{header: make(http.Header)}
	newDocsHandler(fstest.MapFS{}).ServeHTTP(w, httptest.NewRequest("GET", "/missing", nil))
	if w.writes != 1 || len(w.statuses) != 1 || w.statuses[0] != 404 {
		t.Fatalf("fallback appended: writes=%d statuses=%v", w.writes, w.statuses)
	}
}
