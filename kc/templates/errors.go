package templates

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
)

// ErrorPage contains trusted display copy, never raw request or upstream errors.
type ErrorPage struct {
	Title, Message, Guidance       string
	ActionURL, ActionLabel, Footer string
}

var errorPageTemplate, errorPageParseError = template.ParseFS(FS, "base.html", "brand_assets.html", "login_error.html")

// RenderError shares the Kite layout across browser errors. Buffering keeps
// template failures from committing a partial page or the wrong HTTP status.
func RenderError(w http.ResponseWriter, status int, page ErrorPage) error {
	if errorPageParseError != nil {
		return errorPageParseError
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	return RenderPage(w, status, errorPageTemplate, page)
}

// WriteError means headers may already be committed. Callers must only log it,
// never append a fallback page or attempt to change the response status.
type WriteError struct{ Err error }

func (e *WriteError) Error() string { return e.Err.Error() }
func (e *WriteError) Unwrap() error { return e.Err }
func IsWriteError(err error) bool {
	var writeErr *WriteError
	return errors.As(err, &writeErr)
}

// RenderPage buffers execution so only template failures allow a fallback.
func RenderPage(w http.ResponseWriter, status int, tmpl *template.Template, data any) error {
	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, "base", data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := body.WriteTo(w); err != nil {
		return &WriteError{Err: err}
	}
	return nil
}
