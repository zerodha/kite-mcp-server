//go:build testing

package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
	"github.com/zerodha/kite-mcp-server/mcp"
	"github.com/zerodha/kite-mcp-server/oauth"
)

const mockDir = "../mock_responses"

// mockKiteServer creates a test HTTP server that serves kiteconnect-mocks responses.
func mockKiteServer(t *testing.T) *httptest.Server {
	t.Helper()

	// Map route patterns to mock response files.
	routes := map[string]string{
		"GET /user/profile":               "profile.json",
		"GET /user/margins":               "margins.json",
		"GET /user/margins/equity":        "margins_equity.json",
		"GET /portfolio/holdings":         "holdings.json",
		"GET /portfolio/holdings/summary": "holdings_summary.json",
		"GET /portfolio/holdings/compact": "holdings_compact.json",
		"GET /portfolio/positions":        "positions.json",
		"GET /orders":                     "orders.json",
		"GET /trades":                     "trades.json",
		"GET /gtt/triggers":               "gtt_get_orders.json",
		"GET /mf/holdings":                "mf_holdings.json",
		"GET /instruments":                "instruments_all.csv",
		"POST /orders/regular":            "order_response.json",
		"PUT /orders/regular/test":        "order_modify.json",
		"DELETE /orders/regular/test":     "order_cancel.json",
		"POST /gtt/triggers":              "gtt_place_order.json",
		"DELETE /gtt/triggers/123":        "gtt_delete_order.json",
		"GET /alerts":                     "alerts_get.json",
	}

	// Quote/LTP/OHLC endpoints need query string handling.
	quoteRoutes := map[string]string{
		"GET /quote":      "quote.json",
		"GET /quote/ltp":  "ltp.json",
		"GET /quote/ohlc": "ohlc.json",
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path

		if key == "POST /alerts" {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid form", http.StatusBadRequest)
				return
			}
			if r.FormValue("type") == "ato" {
				serveMockFile(t, w, "alerts_create_ato.json")
			} else {
				serveMockFile(t, w, "alerts_create.json")
			}
			return
		}

		if key == "DELETE /alerts" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":null}`))
			return
		}

		// Try exact match first.
		if file, ok := routes[key]; ok {
			serveMockFile(t, w, file)
			return
		}

		// Try quote routes (ignore query params).
		for pattern, file := range quoteRoutes {
			if key == pattern || strings.HasPrefix(key, pattern+"?") {
				serveMockFile(t, w, file)
				return
			}
		}

		// Try prefix matches for parameterized routes.
		prefixRoutes := map[string]string{
			"GET /orders/":                 "order_info.json",
			"GET /gtt/triggers/":           "gtt_get_order.json",
			"PUT /gtt/triggers/":           "gtt_modify_order.json",
			"DELETE /gtt/triggers/":        "gtt_delete_order.json",
			"GET /instruments/historical/": "historical_minute.json",
		}
		for prefix, file := range prefixRoutes {
			if strings.HasPrefix(key, prefix) {
				serveMockFile(t, w, file)
				return
			}
		}

		t.Logf("Unmatched mock route: %s %s", r.Method, r.URL.String())
		http.NotFound(w, r)
	}))
}

func serveMockFile(t *testing.T, w http.ResponseWriter, filename string) {
	t.Helper()
	data, err := os.ReadFile(path.Join(mockDir, filename))
	if err != nil {
		t.Logf("Mock file not found: %s", filename)
		http.Error(w, "mock not found", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// testHarness sets up a full MCP server backed by mock Kite responses
// and returns a connected MCP client session.
type testHarness struct {
	mcpServer   *httptest.Server
	kiteServer  *httptest.Server
	session     *mcpsdk.ClientSession
	client      *mcpsdk.Client
	oauthServer *oauth.Server
	kcManager   *kc.Manager
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Start mock Kite API server.
	kiteServer := mockKiteServer(t)

	// 2. Create instruments manager (empty for speed, search tests can populate).
	instManager, err := instruments.New(instruments.Config{Logger: logger})
	require.NoError(t, err)

	// 3. Create KC manager pointing to mock Kite server.
	kcManager, err := kc.New(kc.Config{
		APIKey:      "test_api_key",
		APISecret:   "test_api_secret",
		Logger:      logger,
		Instruments: instManager,
		KiteBaseURI: kiteServer.URL,
	})
	require.NoError(t, err)

	// 4. Pre-seed a session with valid credentials.
	sess, _, err := kcManager.SessionManager().GetOrCreate("test-session")
	require.NoError(t, err)
	err = kcManager.SessionManager().UpdateCredentials(sess.ID, &kc.KiteCredentials{
		AccessToken: "test_access_token",
		UserID:      "AB1234",
		ExpiresAt:   time.Now().Add(12 * time.Hour),
	})
	require.NoError(t, err)

	// 5. Create OAuth server and generate a test JWT.
	oauthSrv := oauth.New(oauth.Config{
		Issuer:    "http://localhost:0",
		JWTSecret: []byte("test-jwt-secret-at-least-32-bytes-long"),
		TokenTTL:  24 * time.Hour,
	})

	// 6. Create and register MCP tools.
	mcpSrv := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "kite-mcp-test", Version: "test"},
		nil,
	)
	mcp.RegisterTools(mcpSrv, kcManager, "", logger)

	// 7. Create HTTP handler with OAuth middleware.
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(r *http.Request) *mcpsdk.Server { return mcpSrv },
		&mcpsdk.StreamableHTTPOptions{
			Logger:         logger,
			SessionTimeout: 5 * time.Minute,
		},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", oauthSrv.Middleware(http.HandlerFunc(handler.ServeHTTP)))
	mcpHTTPServer := httptest.NewServer(mux)

	// 8. Generate a valid JWT for the test session.
	token := generateTestJWT(t, oauthSrv, "AB1234", "test-session")

	// 9. Connect MCP client with the JWT.
	mcpClient := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "test-client", Version: "test"},
		nil,
	)
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint: mcpHTTPServer.URL + "/mcp",
		HTTPClient: &http.Client{
			Transport: &bearerTokenTransport{token: token},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientSession, err := mcpClient.Connect(ctx, transport, nil)
	require.NoError(t, err, "Failed to connect MCP client")

	h := &testHarness{
		mcpServer:   mcpHTTPServer,
		kiteServer:  kiteServer,
		session:     clientSession,
		client:      mcpClient,
		oauthServer: oauthSrv,
		kcManager:   kcManager,
	}

	t.Cleanup(func() {
		clientSession.Close()
		mcpHTTPServer.Close()
		kiteServer.Close()
		kcManager.Shutdown()
	})

	return h
}

// bearerTokenTransport adds Authorization header to all requests.
type bearerTokenTransport struct {
	token string
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// generateTestJWT creates a valid JWT for testing.
func generateTestJWT(t *testing.T, srv *oauth.Server, userID, sessionID string) string {
	t.Helper()
	token, err := srv.GenerateTestToken(userID, sessionID)
	require.NoError(t, err)
	return token
}

// --- Tests ---

func TestToolListing(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.ListTools(ctx, nil)
	require.NoError(t, err)

	// Verify all 7 tools are present.
	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	expected := []string{"portfolio", "orders", "gtt", "market", "alerts", "mutual_funds", "session"}
	for _, name := range expected {
		assert.True(t, toolNames[name], "Missing tool: %s", name)
	}
	assert.Equal(t, 7, len(result.Tools), "Expected exactly 7 tools")
}

func TestPortfolio_Profile(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "portfolio",
		Arguments: map[string]any{"mode": "profile"},
	})
	require.NoError(t, err)
	assertTextContentContains(t, result, "user_id")
}

func TestPortfolio_Margins(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "portfolio",
		Arguments: map[string]any{"mode": "margins"},
	})
	require.NoError(t, err)
	assertTextContentContains(t, result, "equity")
}

func TestPortfolio_Holdings(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "portfolio",
		Arguments: map[string]any{"mode": "holdings"},
	})
	require.NoError(t, err)
	assertTextContentContains(t, result, "tradingsymbol")
}

func TestPortfolio_Positions(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "portfolio",
		Arguments: map[string]any{"mode": "positions"},
	})
	require.NoError(t, err)
	assertNotError(t, result)
}

func TestOrders_List(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "orders",
		Arguments: map[string]any{"mode": "list"},
	})
	require.NoError(t, err)
	assertTextContentContains(t, result, "order_id")
}

func TestOrders_AllTrades(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "orders",
		Arguments: map[string]any{"mode": "all_trades"},
	})
	require.NoError(t, err)
	assertNotError(t, result)
}

func TestOrders_Place(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "orders",
		Arguments: map[string]any{
			"mode":             "place",
			"exchange":         "NSE",
			"tradingsymbol":    "INFY",
			"transaction_type": "BUY",
			"quantity":         float64(1),
			"product":          "CNC",
			"order_type":       "MARKET",
		},
	})
	require.NoError(t, err)
	assertTextContentContains(t, result, "order_id")
}

func TestOrders_InvalidMode(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "orders",
		Arguments: map[string]any{"mode": "invalid"},
	})
	require.NoError(t, err)
	assertIsError(t, result)
}

func TestGTT_List(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "gtt",
		Arguments: map[string]any{"mode": "list"},
	})
	require.NoError(t, err)
	assertNotError(t, result)
}

func TestGTT_Place(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "gtt",
		Arguments: map[string]any{
			"mode":             "place",
			"exchange":         "NSE",
			"tradingsymbol":    "INFY",
			"last_price":       float64(1500),
			"transaction_type": "BUY",
			"product":          "CNC",
			"trigger_type":     "single",
			"trigger_value":    float64(1400),
			"quantity":         float64(1),
			"limit_price":      float64(1400),
		},
	})
	require.NoError(t, err)
	assertTextContentContains(t, result, "trigger_id")
}

func TestMarket_Quote(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "market",
		Arguments: map[string]any{
			"mode":        "quote",
			"instruments": []any{"NSE:INFY"},
		},
	})
	require.NoError(t, err)
	assertNotError(t, result)
}

func TestMarket_LTP(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "market",
		Arguments: map[string]any{
			"mode":        "ltp",
			"instruments": []any{"NSE:INFY"},
		},
	})
	require.NoError(t, err)
	assertNotError(t, result)
}

func TestMarket_OHLC(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "market",
		Arguments: map[string]any{
			"mode":        "ohlc",
			"instruments": []any{"NSE:INFY"},
		},
	})
	require.NoError(t, err)
	assertNotError(t, result)
}

func TestAlerts_Get(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "alerts",
		Arguments: map[string]any{
			"mode":  "get",
			"uuids": []any{},
		},
	})
	require.NoError(t, err)
	assertNotError(t, result)
}

func TestAlerts_CreateATO(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	basket := `{"name":"buy gold","type":"basket","tags":[],"items":[{"type":"instrument","tradingsymbol":"GOLDBEES","exchange":"NSE","weight":10000,"instrument_token":3693569,"params":{"transaction_type":"BUY","product":"CNC","order_type":"LIMIT","validity":"DAY","validity_ttl":0,"quantity":10000,"price":72.22,"trigger_price":0,"disclosed_quantity":0,"last_price":72.22,"variety":"regular","tags":[]}}]}`

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "alerts",
		Arguments: map[string]any{
			"mode":              "create",
			"uuids":             []any{},
			"name":              "buy gold",
			"alert_type":        "ato",
			"lhs_exchange":      "NSE",
			"lhs_tradingsymbol": "GOLDBEES",
			"lhs_attribute":     "LastTradedPrice",
			"operator":          "<=",
			"rhs_type":          "constant",
			"rhs_constant":      float64(71.8),
			"basket":            basket,
		},
	})
	require.NoError(t, err)
	assertNotError(t, result)
	assertTextContentContains(t, result, `"type":"ato"`)
}

func TestAlerts_CreateATORequiresBasket(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "alerts",
		Arguments: map[string]any{
			"mode":              "create",
			"uuids":             []any{},
			"name":              "buy gold",
			"alert_type":        "ato",
			"lhs_exchange":      "NSE",
			"lhs_tradingsymbol": "GOLDBEES",
			"lhs_attribute":     "LastTradedPrice",
			"operator":          "<=",
			"rhs_type":          "constant",
			"rhs_constant":      float64(71.8),
		},
	})
	require.NoError(t, err)
	assertIsError(t, result)
	assertTextContentContains(t, result, "basket is required when alert_type=ato")
}

func TestAlerts_Delete(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "alerts",
		Arguments: map[string]any{
			"mode":  "delete",
			"uuids": []any{"6cff3180-38cb-463f-8925-6a4d77da5144"},
		},
	})
	require.NoError(t, err)
	assertNotError(t, result)
	assertTextContentContains(t, result, `"deleted_count":1`)
}

func TestSession_Status(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "session",
		Arguments: map[string]any{"mode": "status"},
	})
	require.NoError(t, err)
	assertNotError(t, result)

	response := parseJSONResponse(t, result)
	assert.Equal(t, "authenticated", response["status"])
	assert.Equal(t, true, response["authenticated"])
	assert.Equal(t, "test-session", response["session_id"])
}

func TestSession_Logout(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	logoutResult, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "session",
		Arguments: map[string]any{"mode": "logout"},
	})
	require.NoError(t, err)
	assertNotError(t, logoutResult)
	assertTextContentContains(t, logoutResult, `"status":"logged_out"`)

	statusResult, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "session",
		Arguments: map[string]any{"mode": "status"},
	})
	require.NoError(t, err)
	assertNotError(t, statusResult)
	assertTextContentContains(t, statusResult, `"status":"logged_out"`)

	portfolioResult, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "portfolio",
		Arguments: map[string]any{"mode": "profile"},
	})
	require.NoError(t, err)
	assertIsError(t, portfolioResult)
	assertTextContentContains(t, portfolioResult, "failed to get or create session")
}

func TestMutualFunds_Holdings(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "mutual_funds",
		Arguments: map[string]any{"mode": "holdings"},
	})
	require.NoError(t, err)
	assertNotError(t, result)
}

func TestOrders_MissingRequiredParams(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// Place without required params.
	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "orders",
		Arguments: map[string]any{
			"mode": "place",
			// Missing all required params.
		},
	})
	require.NoError(t, err)
	assertIsError(t, result)
}

func TestPortfolio_InvalidMode(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "portfolio",
		Arguments: map[string]any{"mode": "invalid"},
	})
	require.NoError(t, err)
	assertIsError(t, result)
}

// --- Helpers ---

func assertTextContentContains(t *testing.T, result *mcpsdk.CallToolResult, substring string) {
	t.Helper()
	require.NotNil(t, result)
	require.NotEmpty(t, result.Content)
	for _, c := range result.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			if strings.Contains(tc.Text, substring) {
				return
			}
		}
	}
	// Dump content for debugging.
	for _, c := range result.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			t.Logf("Content: %s", tc.Text[:min(len(tc.Text), 500)])
		}
	}
	t.Errorf("Expected tool result to contain %q", substring)
}

func assertNotError(t *testing.T, result *mcpsdk.CallToolResult) {
	t.Helper()
	require.NotNil(t, result)
	if result.IsError {
		for _, c := range result.Content {
			if tc, ok := c.(*mcpsdk.TextContent); ok {
				t.Errorf("Tool returned error: %s", tc.Text)
			}
		}
	}
}

func assertIsError(t *testing.T, result *mcpsdk.CallToolResult) {
	t.Helper()
	require.NotNil(t, result)
	assert.True(t, result.IsError, "Expected tool to return an error")
}

func getTextContent(result *mcpsdk.CallToolResult) string {
	for _, c := range result.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func parseJSONResponse(t *testing.T, result *mcpsdk.CallToolResult) map[string]interface{} {
	t.Helper()
	text := getTextContent(result)
	var data map[string]interface{}
	err := json.Unmarshal([]byte(text), &data)
	require.NoError(t, err, "Failed to parse tool response as JSON: %s", text[:min(len(text), 200)])
	return data
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
