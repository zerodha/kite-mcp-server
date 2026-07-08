//go:build testing

// mockserver starts a full Kite MCP server backed by kiteconnect-mocks responses.
// Use this for integration testing, LLM eval, or local development without Kite credentials.
//
// Usage:
//
//	go run ./cmd/mockserver
//
// The server prints the MCP endpoint URL and a Bearer token for authentication.
// Connect with pi-mcp-adapter, Claude Desktop, or any MCP client.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/kc/instruments"
	"github.com/zerodha/kite-mcp-server/mcp"
	"github.com/zerodha/kite-mcp-server/oauth"
)

var (
	port     = flag.Int("port", 8989, "port to listen on")
	mockDir  = flag.String("mocks", "mock_responses", "path to kiteconnect-mocks directory")
	jsonOut  = flag.Bool("json", false, "output connection info as JSON (for scripts)")
	logLevel = flag.String("log", "info", "log level: debug, info, warn, error")
)

func main() {
	flag.Parse()

	// Logger.
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	// 1. Start mock Kite API server.
	kiteMockURL := startMockKiteServer(logger, *mockDir)
	logger.Info("Mock Kite API server started", "url", kiteMockURL)

	// 2. Instruments manager (empty - search tests need real data).
	instManager, err := instruments.New(instruments.Config{Logger: logger})
	if err != nil {
		logger.Error("Failed to create instruments manager", "error", err)
		os.Exit(1)
	}

	// 3. KC manager pointing to mock server.
	addr := fmt.Sprintf("http://localhost:%d", *port)
	kcManager, err := kc.New(kc.Config{
		APIKey:      "mock_api_key",
		APISecret:   "mock_api_secret",
		Logger:      logger,
		Instruments: instManager,
		KiteBaseURI: kiteMockURL,
	})
	if err != nil {
		logger.Error("Failed to create KC manager", "error", err)
		os.Exit(1)
	}
	defer kcManager.Shutdown()

	// 4. Pre-seed a session with mock credentials.
	sess, _, err := kcManager.SessionManager().GetOrCreate("mock-session")
	if err != nil {
		logger.Error("Failed to create session", "error", err)
		os.Exit(1)
	}
	err = kcManager.SessionManager().UpdateCredentials(sess.ID, &kc.KiteCredentials{
		AccessToken: "mock_access_token",
		UserID:      "AB1234",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		logger.Error("Failed to set credentials", "error", err)
		os.Exit(1)
	}

	// 5. OAuth server + test JWT.
	oauthSrv := oauth.New(oauth.Config{
		Issuer:    addr,
		JWTSecret: []byte("mock-jwt-secret-at-least-32-bytes!"),
		TokenTTL:  24 * time.Hour,
	})
	token, err := oauthSrv.GenerateTestToken("AB1234", "mock-session")
	if err != nil {
		logger.Error("Failed to generate test JWT", "error", err)
		os.Exit(1)
	}

	// 6. MCP server + tools.
	mcpSrv := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "kite-mcp-mock", Version: "test"},
		nil,
	)
	mcp.RegisterTools(mcpSrv, kcManager, "", logger)

	// 7. HTTP handler.
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(r *http.Request) *mcpsdk.Server { return mcpSrv },
		&mcpsdk.StreamableHTTPOptions{
			Logger:         logger,
			SessionTimeout: 30 * time.Minute,
		},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", oauthSrv.Middleware(http.HandlerFunc(handler.ServeHTTP)))

	// 8. Print connection info.
	endpoint := fmt.Sprintf("http://localhost:%d/mcp", *port)
	if *jsonOut {
		info := map[string]string{
			"endpoint": endpoint,
			"token":    token,
		}
		json.NewEncoder(os.Stdout).Encode(info)
	} else {
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "  MCP endpoint: %s\n", endpoint)
		fmt.Fprintf(os.Stderr, "  Bearer token: %s\n", token)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "  pi-mcp-adapter config (mcp.json):\n")
		fmt.Fprintf(os.Stderr, "  {\n")
		fmt.Fprintf(os.Stderr, "    \"mcpServers\": {\n")
		fmt.Fprintf(os.Stderr, "      \"kite\": {\n")
		fmt.Fprintf(os.Stderr, "        \"url\": \"%s\",\n", endpoint)
		fmt.Fprintf(os.Stderr, "        \"auth\": \"bearer\",\n")
		fmt.Fprintf(os.Stderr, "        \"bearerToken\": \"%s\",\n", token)
		fmt.Fprintf(os.Stderr, "        \"directTools\": true\n")
		fmt.Fprintf(os.Stderr, "      }\n")
		fmt.Fprintf(os.Stderr, "    }\n")
		fmt.Fprintf(os.Stderr, "  }\n")
		fmt.Fprintf(os.Stderr, "\n")
	}

	// 9. Start server.
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		logger.Info("Shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	logger.Info("MCP mock server listening", "port", *port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("Server error", "error", err)
		os.Exit(1)
	}
}

// startMockKiteServer starts an HTTP server serving kiteconnect-mocks responses
// on a random port. Returns the base URL.
func startMockKiteServer(logger *slog.Logger, mockDir string) string {
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
		"POST /orders/regular":            "order_response.json",
		"PUT /orders/regular/test":        "order_modify.json",
		"DELETE /orders/regular/test":     "order_cancel.json",
		"POST /gtt/triggers":              "gtt_place_order.json",
		"DELETE /gtt/triggers/123":        "gtt_delete_order.json",
		"GET /alerts":                     "alerts_get.json",
	}

	quoteRoutes := map[string]string{
		"GET /quote":      "quote.json",
		"GET /quote/ltp":  "ltp.json",
		"GET /quote/ohlc": "ohlc.json",
	}

	prefixRoutes := map[string]string{
		"GET /orders/":                 "order_info.json",
		"GET /gtt/triggers/":           "gtt_get_order.json",
		"PUT /gtt/triggers/":           "gtt_modify_order.json",
		"DELETE /gtt/triggers/":        "gtt_delete_order.json",
		"GET /instruments/historical/": "historical_minute.json",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path

		if key == "POST /alerts" {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid form", http.StatusBadRequest)
				return
			}
			if r.FormValue("type") == "ato" {
				serveMock(w, mockDir, "alerts_create_ato.json")
			} else {
				serveMock(w, mockDir, "alerts_create.json")
			}
			return
		}

		if key == "DELETE /alerts" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":null}`))
			return
		}

		if file, ok := routes[key]; ok {
			serveMock(w, mockDir, file)
			return
		}

		for pattern, file := range quoteRoutes {
			if key == pattern {
				serveMock(w, mockDir, file)
				return
			}
		}

		for prefix, file := range prefixRoutes {
			if strings.HasPrefix(key, prefix) {
				serveMock(w, mockDir, file)
				return
			}
		}

		logger.Warn("Unmatched mock route", "method", r.Method, "path", r.URL.String())
		http.NotFound(w, r)
	})

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		logger.Error("Failed to start mock Kite server", "error", err)
		os.Exit(1)
	}

	go http.Serve(listener, mux)

	port := listener.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("http://localhost:%d", port)
}

func serveMock(w http.ResponseWriter, mockDir, filename string) {
	data, err := os.ReadFile(path.Join(mockDir, filename))
	if err != nil {
		http.Error(w, "mock not found: "+filename, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
