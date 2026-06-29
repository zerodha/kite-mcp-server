package app

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:generate go run github.com/oddship/moat@latest build ../docs ./docs-site

//go:embed all:docs-site
var docsSiteFS embed.FS

// NewDocsHandler returns an http.Handler that serves the moat-generated
// static documentation site from the embedded filesystem.
func NewDocsHandler() http.Handler {
	sub, _ := fs.Sub(docsSiteFS, "docs-site")
	return newDocsHandler(sub)
}

func newDocsHandler(root fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolved, ok := resolveDocsPath(root, r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		data, err := fs.ReadFile(root, resolved)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		http.ServeContent(w, r, path.Base(resolved), time.Time{}, bytes.NewReader(data))
	})
}

func resolveDocsPath(root fs.FS, requestPath string) (string, bool) {
	cleanPath := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	if cleanPath == "." {
		cleanPath = ""
	}

	for _, part := range strings.Split(cleanPath, "/") {
		if strings.HasPrefix(part, ".") {
			return "", false
		}
	}

	if cleanPath == "" {
		cleanPath = "index.html"
	}

	if info, err := fs.Stat(root, cleanPath); err == nil {
		if info.IsDir() {
			indexPath := path.Join(cleanPath, "index.html")
			if info, err := fs.Stat(root, indexPath); err == nil && !info.IsDir() {
				return indexPath, true
			}
			return "", false
		}
		return cleanPath, true
	}

	indexPath := path.Join(cleanPath, "index.html")
	if info, err := fs.Stat(root, indexPath); err == nil && !info.IsDir() {
		return indexPath, true
	}

	return "", false
}
