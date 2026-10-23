package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestHTTPTradeApproval(t *testing.T) {
	for _, version := range []string{"2026-07-28", "2025-11-25", "2025-06-18"} {
		t.Run(version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server := NewServer(&sdk.Implementation{Name: "server", Version: "1"})
			server.AddTool(&sdk.Tool{Name: "approval", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				return withTradeApproval(ctx, req, "orders_place", map[string]any{"quantity": 1}, func() (*sdk.CallToolResult, error) { return NewToolResultText("executed"), nil })
			})
			handler := NewHTTPHandler(server, nil)
			// Match the stable authenticated owner injected by OAuth middleware.
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set("X-Kite-Session-Id", "authenticated-test-session")
				handler.ServeHTTP(w, r)
			}))
			defer endpoint.Close()
			elicited := 0
			client := sdk.NewClient(&sdk.Implementation{Name: "client", Version: "1"}, &sdk.ClientOptions{ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
				elicited++
				return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
			}})
			session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: endpoint.URL}, &sdk.ClientSessionOptions{ProtocolVersion: version})
			require.NoError(t, err)
			defer session.Close()
			require.Equal(t, version, session.InitializeResult().ProtocolVersion)
			result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "approval"})
			require.NoError(t, err)
			require.False(t, result.IsError, "%v", result.Content)
			require.Equal(t, "executed", result.Content[0].(*sdk.TextContent).Text)
			require.Equal(t, 1, elicited)
		})
	}
}
