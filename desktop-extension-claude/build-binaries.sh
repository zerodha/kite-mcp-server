#!/bin/bash

# Build script for cross-compiling kite-mcp-server binaries
# This script should be run from the kite-mcp-server root directory

set -e

BINARY_DIR="desktop-extension-claude/server/binaries"
BINARY_NAME="kite-mcp"
MAIN_FILE="main.go"
LDFLAGS="-s -w"
export CGO_ENABLED=0

# Create binaries directory if it doesn't exist
mkdir -p "$BINARY_DIR"

echo "Building cross-platform binaries..."

# Sync DXT extension version first
echo "Syncing extension version..."
"$(dirname "$0")/sync-version.sh"
echo ""

# Get version from git or use 'dev'
VERSION=$(git describe --tags --dirty --always 2>/dev/null || echo "dev")
BUILD_STRING="$(date -u '+%Y-%m-%d %H:%M:%S UTC') - $(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"

echo "Building version: $VERSION"
echo "Build string: $BUILD_STRING"

# Build flags with version info
BUILD_LDFLAGS="$LDFLAGS -X 'main.MCP_SERVER_VERSION=$VERSION' -X 'main.buildString=$BUILD_STRING'"

# macOS AMD64
echo "Building for macOS AMD64..."
GOOS=darwin GOARCH=amd64 go build -o "$BINARY_DIR/kite-mcp-darwin-amd64" -ldflags="$BUILD_LDFLAGS" "$MAIN_FILE"

# macOS ARM64
echo "Building for macOS ARM64..."
GOOS=darwin GOARCH=arm64 go build -o "$BINARY_DIR/kite-mcp-darwin-arm64" -ldflags="$BUILD_LDFLAGS" "$MAIN_FILE"

# Windows AMD64
echo "Building for Windows AMD64..."
GOOS=windows GOARCH=amd64 go build -o "$BINARY_DIR/kite-mcp-windows-amd64.exe" -ldflags="$BUILD_LDFLAGS" "$MAIN_FILE"

# Linux AMD64
echo "Building for Linux AMD64..."
GOOS=linux GOARCH=amd64 go build -o "$BINARY_DIR/kite-mcp-linux-amd64" -ldflags="$BUILD_LDFLAGS" "$MAIN_FILE"

# Linux ARM64
echo "Building for Linux ARM64..."
GOOS=linux GOARCH=arm64 go build -o "$BINARY_DIR/kite-mcp-linux-arm64" -ldflags="$BUILD_LDFLAGS" "$MAIN_FILE"

echo "✅ All binaries built successfully!"
echo "Binaries created in: $BINARY_DIR"
ls -la "$BINARY_DIR"

# Make binaries executable
chmod +x "$BINARY_DIR"/*

echo "✅ Cross-compilation complete!"
echo ""
echo "To use the extension:"
echo "1. Install dxt CLI: npm install -g @anthropic-ai/dxt"
echo "2. Package the extension: dxt pack desktop-extension-claude"
echo "3. Install in Claude Desktop"