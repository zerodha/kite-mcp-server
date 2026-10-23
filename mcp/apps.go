package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"maps"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/kc"
)

const (
	appExtension   = "io.modelcontextprotocol/ui"
	appResourceURI = "ui://kite/explorer-v1.html"
	appMIMEType    = "text/html;profile=mcp-app"
	appLauncher    = "kite_explorer"
)

// The committed bundle keeps ordinary Go builds independent of Node and CDNs.
// Rebuild after UI changes with: npm --prefix mcp/ui ci && npm --prefix mcp/ui run build
//
//go:embed ui/app.html
var appHTML string

func registerAppResource(server *mcp.Server) {
	// Filter a per-request copy: one server serves mixed client capabilities.
	// Never modify registered definitions based on the last client to connect.
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if method != "tools/list" || err != nil {
				return result, err
			}
			request, ok := req.(*mcp.ListToolsRequest)
			if !ok || supportsApps(requestCapabilities(request)) {
				return result, err
			}
			if listing, ok := result.(*mcp.ListToolsResult); ok {
				copy := *listing
				copy.Tools = make([]*mcp.Tool, 0, len(listing.Tools))
				for _, tool := range listing.Tools {
					if tool.Name == appLauncher {
						continue
					}
					cloned := *tool
					cloned.Meta = maps.Clone(tool.Meta)
					delete(cloned.Meta, "ui")
					delete(cloned.Meta, "ui/resourceUri")
					delete(cloned.Meta, "openai/ui")
					copy.Tools = append(copy.Tools, &cloned)
				}
				return &copy, nil
			}
			return result, err
		}
	})
	metadata := func() mcp.Meta {
		return mcp.Meta{"openai/ui": map[string]any{
			"availableDisplayModes": []string{"inline", "fullscreen"},
			"preferredDisplayMode":  "inline",
		}, "ui": map[string]any{
			"prefersBorder": true,
			"csp":           map[string]any{"connectDomains": []string{}, "resourceDomains": []string{}},
		}}
	}
	server.AddResource(&mcp.Resource{
		URI: appResourceURI, Name: "kite-explorer", Title: "Kite portfolio and market explorer",
		Description: "Read-only portfolio tables and historical price charts.",
		MIMEType:    appMIMEType, Meta: metadata(),
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		// This template contains no account data or credentials. All authenticated
		// reads go through the host's tools/call bridge and existing OAuth middleware.
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: appResourceURI, MIMEType: appMIMEType, Text: appHTML, Meta: metadata(),
		}}}, nil
	})
}

// Entrypoints receive {} from the host. Keep the portfolio tool's required
// arguments intact and route this app-only launcher through its authenticated
// handler with an explicit, bounded initial snapshot.
func registerAppLauncher(server *mcp.Server, manager *kc.Manager) {
	tool := NewTool(appLauncher, "Open the read-only Kite portfolio explorer.", json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`))
	tool.Title = "Kite explorer"
	tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
	tool.Meta = mcp.Meta{
		"ui":             map[string]any{"resourceUri": appResourceURI, "visibility": []string{"app"}},
		"ui/resourceUri": appResourceURI,
		"openai/ui":      map[string]any{"entrypoints": []map[string]any{{"type": "global"}, {"type": "thread"}}},
	}
	portfolio := withAppData("portfolio", (&PortfolioTool{}).Handler(manager))
	server.AddTool(tool, wrapHandler(func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		copy := *request
		params := *request.Params
		params.Name = "portfolio"
		params.Arguments = json.RawMessage(`{"mode":"holdings","type":"full","from":0,"limit":100}`)
		copy.Params = &params
		result, err := portfolio(ctx, &copy)
		if err == nil && result != nil {
			info := result.Meta["trade.kite/app"].(map[string]any)
			info["origin"] = map[string]any{"tool": appLauncher, "arguments": GetArguments(request)}
		}
		return result, err
	}))
}

func supportsApps(caps *mcp.ClientCapabilities) bool {
	if caps == nil {
		return false
	}
	// Round-trip supports both typed settings used by Go clients and the map
	// representation decoded from real initialize/request metadata.
	raw, err := json.Marshal(caps.Extensions[appExtension])
	if err != nil {
		return false
	}
	var settings struct {
		MIMETypes []string `json:"mimeTypes"`
	}
	return json.Unmarshal(raw, &settings) == nil && slices.Contains(settings.MIMETypes, appMIMEType)
}

// Preserve text response contracts while adding object structuredContent for
// Apps hosts. Market already supplies structured data; never replace it.
func withAppData(toolName string, next ToolHandler) ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := next(ctx, request)
		if err != nil || result == nil {
			return result, err
		}
		if result.Meta == nil {
			result.Meta = mcp.Meta{}
		}
		result.Meta["trade.kite/app"] = map[string]any{"tool": toolName, "arguments": GetArguments(request)}
		if result.IsError || result.StructuredContent != nil {
			return result, nil
		}
		if len(result.Content) == 1 {
			if content, ok := result.Content[0].(*mcp.TextContent); ok {
				var data any
				if json.Unmarshal([]byte(content.Text), &data) == nil {
					if object, ok := data.(map[string]any); ok {
						result.StructuredContent = object
					} else {
						result.StructuredContent = map[string]any{"data": data}
					}
				}
			}
		}
		return result, nil
	}
}
