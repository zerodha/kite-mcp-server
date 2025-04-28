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
	port := ":8080"
	srv := &http.Server{Addr: port} // TODO: make this configurable and optional

	switch APP_MODE {
	case APP_MODE_SSE:
		log.Println("Starting SSE MCP server... ", port)
		sse := server.NewSSEServer(s, server.WithBaseURL("http://localhost"+port))

		mux := http.NewServeMux()
		mux.HandleFunc("/callback", kcManager.HandleKiteCallback())
		mux.HandleFunc("/sse", sse.ServeHTTP)
		mux.HandleFunc("/message", sse.ServeHTTP)
		srv.Handler = mux

		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case APP_MODE_STDIO:
		log.Println("Starting STDIO MCP server...")
		stdio := server.NewStdioServer(s)

		http.HandleFunc("/callback", kcManager.HandleKiteCallback())

		go srv.ListenAndServe()

		if err := stdio.Listen(context.Background(), os.Stdin, os.Stdout); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}
