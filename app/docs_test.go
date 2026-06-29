package app

import (
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
