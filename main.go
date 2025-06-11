// Kite MCP Server implements the Model Context Protocol for the Kite Connect trading API
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/zerodha/kite-mcp-server/app"
)

var (
	// MCP_SERVER_VERSION will be injected during the build process by the justfile
	// Use 'just build-version VERSION' to set a specific version
	MCP_SERVER_VERSION = "v0.0.0"

	// buildString will be injected during the build process with build time and git info
	buildString = "dev build"
)

func initLogger() *slog.Logger {
	// Default to INFO level, can be overridden by LOG_LEVEL env var
	// Valid levels: debug, info, warn, error
	var level slog.Level
	logLevel := os.Getenv("LOG_LEVEL")
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info", "":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo // Default to INFO if invalid
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewTextHandler(os.Stderr, opts)
	return slog.New(handler)
}

func main() {
	// Check for version flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("Kite MCP Server %s\n", MCP_SERVER_VERSION)
		fmt.Printf("Build: %s\n", buildString)
		os.Exit(0)
	}

	// Initialize logger
	logger := initLogger()

	// Create a new application instance
	application := app.NewApp(logger)

	// Load configuration from environment
	if err := application.LoadConfig(); err != nil {
		logger.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Set the server version
	application.SetVersion(MCP_SERVER_VERSION)

	// Run the server (blocks until shutdown)
	logger.Info("Starting Kite MCP Server...", "version", MCP_SERVER_VERSION, "build", buildString)
	if err := application.RunServer(); err != nil {
		logger.Error("Server failed to start", "error", err)
		os.Exit(1)
	}
}
