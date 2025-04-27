package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"github.com/zerodha/kite-mcp-server/kc"
	"github.com/zerodha/kite-mcp-server/mcp"
)

const (
	APP_MODE_SSE   = "sse"
	APP_MODE_STDIO = "stdio"
)

func main() {
	var (
		KITE_API_KEY    = os.Getenv("KITE_API_KEY")
		KITE_API_SECRET = os.Getenv("KITE_API_SECRET")
		APP_MODE        = os.Getenv("APP_MODE")
	)

	// Set default mode if not specified
	if APP_MODE == "" {
		APP_MODE = APP_MODE_SSE
	}

	// Check if API KEY or SECRET is missing
	if KITE_API_KEY == "" || KITE_API_SECRET == "" {
		log.Fatal("KITE_API_KEY or KITE_API_SECRET is missing")
	}

	kcManager := kc.NewManager(
		KITE_API_KEY,
		KITE_API_SECRET,
	)

	// Create MCP server
	s := server.NewMCPServer(
		"Kite MCP Server",
		"1.0.0",
	)

	// Add tool
	mcp.RegisterTools(s, kcManager)

	// Start the server for receiving callbacks
	log.Println("Starting kite connect callback server")
	srv := &http.Server{Addr: ":8080"}                                                         // TODO: make this configurable and optional
	http.HandleFunc("/api/user/callback/kite/", func(w http.ResponseWriter, r *http.Request) { // TODO: The handler needs to be configurable
		requestToken := r.URL.Query()["request_token"][0]
		sessionID := r.URL.Query()["session_id"][0] // TODO: think of hashing this with some secret so that it cant be tampered.

		if err := kcManager.GenerateSession(sessionID, requestToken); err != nil {
			log.Println("error generating session", err)
			http.Error(w, "error generating session", http.StatusInternalServerError)
			return
		}

		w.Write([]byte("login successful!"))
		return
	})
	go srv.ListenAndServe()

	switch APP_MODE {
	case APP_MODE_SSE:
		port := ":8081"
		log.Println("Starting SSE MCP server... ", port)
		sse := server.NewSSEServer(s, server.WithBaseURL("http://localhost"+port))
		if err := sse.Start(port); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case APP_MODE_STDIO:
		log.Println("Starting STDIO MCP server...")
		stdio := server.NewStdioServer(s)

		if err := stdio.Listen(context.Background(), os.Stdin, os.Stdout); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}
