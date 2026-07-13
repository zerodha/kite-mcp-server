#!/bin/bash

# Version sync script for Kite Connect Desktop Extension
# Synchronizes the DXT manifest version with the git-derived MCP server version

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST_FILE="$SCRIPT_DIR/manifest.json"

echo "🔄 Syncing DXT extension version with git tags..."

# Check if manifest.json exists
if [ ! -f "$MANIFEST_FILE" ]; then
    echo "❌ Error: manifest.json not found at $MANIFEST_FILE"
    exit 1
fi

# Check if jq is available
if ! command -v jq >/dev/null 2>&1; then
    echo "❌ Error: jq is required but not installed. Please install jq."
    echo "   macOS: brew install jq"
    echo "   Ubuntu/Debian: apt-get install jq"
    exit 1
fi

# Get version from git (same logic as MCP server)
GIT_VERSION=$(git describe --tags --dirty --always 2>/dev/null || echo "dev")
echo "📋 Git version: $GIT_VERSION"

# Convert git version to DXT-compatible format
# v0.2.0 -> 0.2.0
# v0.2.0-dev4 -> 0.2.0-dev4  
# v0.2.0-dev4-1-g4113eba -> 0.2.0-dev4.1
# dev -> 0.0.0-dev
if [[ "$GIT_VERSION" == "dev" ]]; then
    DXT_VERSION="0.0.0-dev"
elif [[ "$GIT_VERSION" =~ ^v?([0-9]+\.[0-9]+\.[0-9]+)(-[a-zA-Z0-9]+)?(-[0-9]+)?(-g[a-f0-9]+)?(-dirty)?$ ]]; then
    # Extract version components
    MAIN_VERSION="${BASH_REMATCH[1]}"
    PRE_RELEASE="${BASH_REMATCH[2]}"
    COMMIT_COUNT="${BASH_REMATCH[3]}"
    GIT_HASH="${BASH_REMATCH[4]}"
    DIRTY="${BASH_REMATCH[5]}"
    
    # Build DXT version
    DXT_VERSION="$MAIN_VERSION"
    
    # Add pre-release if present
    if [ -n "$PRE_RELEASE" ]; then
        DXT_VERSION="$DXT_VERSION$PRE_RELEASE"
    fi
    
    # Add commit count as patch version if present
    if [ -n "$COMMIT_COUNT" ]; then
        COMMIT_NUM="${COMMIT_COUNT#-}"  # Remove leading dash
        if [ -n "$PRE_RELEASE" ]; then
            DXT_VERSION="$DXT_VERSION.$COMMIT_NUM"
        else
            DXT_VERSION="$DXT_VERSION-dev.$COMMIT_NUM"
        fi
    fi
    
    # Add dirty suffix if present
    if [ -n "$DIRTY" ]; then
        DXT_VERSION="$DXT_VERSION-dirty"
    fi
else
    # Fallback for unexpected format
    echo "⚠️  Warning: Unexpected git version format '$GIT_VERSION', using as-is"
    DXT_VERSION="${GIT_VERSION#v}"  # Remove 'v' prefix if present
fi

echo "🎯 DXT version: $DXT_VERSION"

# Get current version from manifest
CURRENT_VERSION=$(jq -r '.version' "$MANIFEST_FILE")
echo "📖 Current manifest version: $CURRENT_VERSION"

# Update manifest.json if version changed
if [ "$CURRENT_VERSION" != "$DXT_VERSION" ]; then
    echo "📝 Updating manifest.json version: $CURRENT_VERSION → $DXT_VERSION"
    
    # Create backup
    cp "$MANIFEST_FILE" "$MANIFEST_FILE.backup"
    
    # Update version using jq
    jq ".version = \"$DXT_VERSION\"" "$MANIFEST_FILE.backup" > "$MANIFEST_FILE"
    
    # Verify the update
    NEW_VERSION=$(jq -r '.version' "$MANIFEST_FILE")
    if [ "$NEW_VERSION" = "$DXT_VERSION" ]; then
        echo "✅ Version successfully updated to $DXT_VERSION"
        rm "$MANIFEST_FILE.backup"
    else
        echo "❌ Error: Version update failed"
        mv "$MANIFEST_FILE.backup" "$MANIFEST_FILE"
        exit 1
    fi
else
    echo "✅ Version already up to date ($DXT_VERSION)"
fi

echo "🎉 Version sync complete!"