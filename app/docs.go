package app

import (
	"bytes"
	"embed"
	"github.com/zerodha/kite-mcp-server/kc/templates"
	"io/fs"
	"log/slog"
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
			renderDocsNotFound(w, r)
			return
		}

		data, err := fs.ReadFile(root, resolved)
		if err != nil {
			renderDocsNotFound(w, r)
			return
		}

		http.ServeContent(w, r, path.Base(resolved), time.Time{}, bytes.NewReader(data))
	})
}

func renderDocsNotFound(w http.ResponseWriter, r *http.Request) {
	slog.Debug("documentation page not found", "path", r.URL.Path, "status", http.StatusNotFound)
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := templates.RenderError(w, http.StatusNotFound, templates.ErrorPage{
		Title:     "Page not found",
		Message:   "The page you're looking for isn't available.",
		Guidance:  "Check the address or return to the documentation home page.",
		ActionURL: "/", ActionLabel: "Go to documentation",
	}); err != nil {
		slog.Error("failed to render documentation error", "error", err)
		if templates.IsWriteError(err) {
			return
		}
		http.Error(w, "Page not found. Visit / for documentation.", http.StatusNotFound)
	}
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
