package mcp

import (
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPHandler serves modern stateless requests alongside legacy sessions.
// Legacy clients retain stateful transport for nested trade elicitation.
func NewHTTPHandler(server *sdk.Server, options *sdk.StreamableHTTPOptions) http.Handler {
	var legacyOptions sdk.StreamableHTTPOptions
	if options != nil {
		legacyOptions = *options
	}
	legacyOptions.Stateless = false
	modernOptions := legacyOptions
	modernOptions.Stateless = true
	getServer := func(*http.Request) *sdk.Server { return server }
	legacy := sdk.NewStreamableHTTPHandler(getServer, &legacyOptions)
	modern := sdk.NewStreamableHTTPHandler(getServer, &modernOptions)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("MCP-Protocol-Version") >= "2026-07-28" {
			modern.ServeHTTP(w, r)
			return
		}
		legacy.ServeHTTP(w, r)
	})
}
