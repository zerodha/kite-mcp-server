package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestAppsMixedClientDiscoveryAndReads(t *testing.T) {
	manager := readContractManager(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/portfolio/holdings", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","data":[{"tradingsymbol":"INFY","exchange":"NSE","quantity":3,"average_price":1500,"last_price":1600}]}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := NewServer(&sdk.Implementation{Name: "apps-test", Version: "1"})
	RegisterTools(server, manager, "orders", slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if call, ok := req.(*sdk.CallToolRequest); ok {
				call.Extra = &sdk.RequestExtra{Header: http.Header{"X-Kite-Session-Id": []string{"read-contract"}}}
			}
			return next(ctx, method, req)
		}
	})
	// Keep clients connected concurrently and alternate their tool listings.
	var sessions []*sdk.ClientSession
	for _, mime := range []string{appMIMEType, "", "text/plain", appMIMEType} {
		caps := &sdk.ClientCapabilities{}
		if mime != "" {
			caps.AddExtension(appExtension, map[string]any{"mimeTypes": []string{mime}})
		}
		client := sdk.NewClient(&sdk.Implementation{Name: "client", Version: "1"}, &sdk.ClientOptions{Capabilities: caps})
		ct, st := sdk.NewInMemoryTransports()
		ss, err := server.Connect(ctx, st, nil)
		require.NoError(t, err)
		defer func() { require.NoError(t, ss.Close()) }()
		cs, err := client.Connect(ctx, ct, &sdk.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
		require.NoError(t, err)
		defer func() { require.NoError(t, cs.Close()) }()
		require.Contains(t, cs.InitializeResult().Capabilities.Extensions, appExtension)
		require.NotNil(t, cs.InitializeResult().Capabilities.Resources)
		sessions = append(sessions, cs)
	}
	for round := 0; round < 2; round++ {
		for index, cs := range sessions {
			listing, err := cs.ListTools(ctx, nil)
			require.NoError(t, err)
			require.Len(t, listing.Tools, len(GetAllTools())-1)
			for _, tool := range listing.Tools {
				require.NotEqual(t, "orders", tool.Name)
				if (index == 0 || index == 3) && (tool.Name == "portfolio" || tool.Name == "market") {
					ui := tool.Meta["ui"].(map[string]any)
					require.Equal(t, appResourceURI, ui["resourceUri"])
					require.Equal(t, appResourceURI, tool.Meta["ui/resourceUri"])
				} else {
					require.NotContains(t, tool.Meta, "ui")
				}
			}
			result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "portfolio", Arguments: map[string]any{"mode": "holdings", "limit": 1}})
			require.NoError(t, err)
			require.False(t, result.IsError)
			var text map[string]any
			require.NoError(t, json.Unmarshal([]byte(result.Content[0].(*sdk.TextContent).Text), &text))
			require.Equal(t, text, result.StructuredContent)
			info := result.Meta["trade.kite/app"].(map[string]any)
			require.Equal(t, "portfolio", info["tool"])
			require.Equal(t, "holdings", info["arguments"].(map[string]any)["mode"])
		}
	}
	resources, err := sessions[0].ListResources(ctx, nil)
	require.NoError(t, err)
	found := false
	for _, r := range resources.Resources {
		if r.URI == appResourceURI {
			found = true
			require.Equal(t, appMIMEType, r.MIMEType)
		}
	}
	require.True(t, found)
	resource, err := sessions[0].ReadResource(ctx, &sdk.ReadResourceParams{URI: appResourceURI})
	require.NoError(t, err)
	require.Len(t, resource.Contents, 1)
	require.Equal(t, appMIMEType, resource.Contents[0].MIMEType)
	require.Contains(t, resource.Contents[0].Text, "<!doctype html>")
	require.NotContains(t, resource.Contents[0].Text, "<!-- APP_SCRIPT -->")
	csp := resource.Contents[0].Meta["ui"].(map[string]any)["csp"].(map[string]any)
	require.Empty(t, csp["connectDomains"])
	require.Empty(t, csp["resourceDomains"])
	_, err = sessions[0].ReadResource(ctx, &sdk.ReadResourceParams{URI: "ui://kite/../../.env"})
	require.Error(t, err)
}

func TestAppDataPreservesErrorsAndMarketSchema(t *testing.T) {
	request := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: json.RawMessage(`{"mode":"quote"}`)}}
	for _, original := range []*sdk.CallToolResult{
		NewToolResultError("Login required"),
		{Content: []sdk.Content{&sdk.TextContent{Text: `{"data":[],"meta":{}}`}}, StructuredContent: map[string]any{"data": []any{}, "meta": map[string]any{}}},
	} {
		structured := original.StructuredContent
		handler := withAppData("market", func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) { return original, nil })
		result, err := handler(context.Background(), request)
		require.NoError(t, err)
		require.Same(t, original, result)
		require.Equal(t, structured, result.StructuredContent)
	}
	expected := errors.New("transport failed")
	result, err := withAppData("portfolio", func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) { return nil, expected })(context.Background(), request)
	require.Nil(t, result)
	require.ErrorIs(t, err, expected)
}
