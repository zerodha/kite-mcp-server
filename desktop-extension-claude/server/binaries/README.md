# Binary Files

This directory contains the cross-compiled binaries for the kite-mcp-server.

## Required Binaries

The following binaries should be present for the extension to work:

- `kite-mcp-darwin-amd64` - macOS Intel binary
- `kite-mcp-darwin-arm64` - macOS Apple Silicon binary  
- `kite-mcp-linux-amd64` - Linux x86_64 binary
- `kite-mcp-linux-arm64` - Linux ARM64 binary
- `kite-mcp-windows-amd64.exe` - Windows x86_64 binary

## Building Binaries

To build these binaries, run the build script from the kite-mcp-server root directory:

```bash
./desktop-extension-claude/build-binaries.sh
```

This requires:
- Go 1.21 or later
- Git (for versioning)

## Alternative Build Methods

You can also build using the justfile:

```bash
just build
```

Or build manually:

```bash
# Example for macOS ARM64
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o desktop-extension-claude/server/binaries/kite-mcp-darwin-arm64 -ldflags="-s -w" main.go
```

## Testing

To test the extension without building all binaries, you can create a symbolic link to a locally built binary:

```bash
# Build for current platform
just build

# Link to binaries directory (adjust target name based on your platform)
ln -s ../../../../kite-mcp.bin desktop-extension-claude/server/binaries/kite-mcp-darwin-arm64
```