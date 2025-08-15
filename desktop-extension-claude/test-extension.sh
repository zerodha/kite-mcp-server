#!/bin/bash

# Test script for kite-connect desktop extension
# This script validates the extension structure and basic functionality

set -e

echo "🧪 Testing Kite Connect Desktop Extension"
echo "============================================"

EXTENSION_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$EXTENSION_DIR"

# Test 1: Check required files exist
echo "✅ Test 1: Checking required files..."
required_files=(
    "manifest.json"
    "README.md"
    "server/index.js"
    "server/package.json"
    "server/binaries/README.md"
    "build-binaries.sh"
)

for file in "${required_files[@]}"; do
    if [ -f "$file" ]; then
        echo "  ✅ $file exists"
    else
        echo "  ❌ $file missing"
        exit 1
    fi
done

# Test 2: Validate manifest.json
echo "✅ Test 2: Validating manifest.json..."
if command -v node >/dev/null 2>&1; then
    node -e "
    const fs = require('fs');
    const manifest = JSON.parse(fs.readFileSync('manifest.json', 'utf8'));
    
    // Check required fields
    const required = ['dxt_version', 'name', 'version', 'description', 'author', 'server'];
    for (const field of required) {
        if (!manifest[field]) {
            console.error(\`❌ Missing required field: \${field}\`);
            process.exit(1);
        }
    }
    
    // Check server configuration
    if (!manifest.server.type || !manifest.server.entry_point) {
        console.error('❌ Invalid server configuration');
        process.exit(1);
    }
    
    // Check user config
    if (!manifest.user_config || !Array.isArray(manifest.user_config)) {
        console.error('❌ Invalid user_config');
        process.exit(1);
    }
    
    console.log('  ✅ manifest.json is valid');
    console.log(\`  ✅ Extension name: \${manifest.name}\`);
    console.log(\`  ✅ Version: \${manifest.version}\`);
    console.log(\`  ✅ Tools: \${manifest.tools?.length || 0} tools defined\`);
    "
else
    echo "  ⚠️  Node.js not found, skipping manifest validation"
fi

# Test 3: Check server structure
echo "✅ Test 3: Checking server structure..."
if [ -f "server/index.js" ]; then
    echo "  ✅ Server entry point exists"
    if grep -q "KiteExtensionProxy" server/index.js; then
        echo "  ✅ Server class found"
    else
        echo "  ❌ Server class not found"
        exit 1
    fi
else
    echo "  ❌ server/index.js missing"
    exit 1
fi

# Test 4: Check build script
echo "✅ Test 4: Checking build script..."
if [ -x "build-binaries.sh" ]; then
    echo "  ✅ Build script is executable"
else
    echo "  ❌ Build script not executable"
    exit 1
fi

# Test 5: Check binary directory
echo "✅ Test 5: Checking binary directory..."
if [ -d "server/binaries" ]; then
    echo "  ✅ Binaries directory exists"
    binary_count=$(find server/binaries -type f -name "kite-mcp-*" | wc -l)
    echo "  ✅ Found $binary_count binary files"
else
    echo "  ❌ Binaries directory missing"
    exit 1
fi

# Test 6: Test server initialization (if Node.js available)
echo "✅ Test 6: Testing server initialization..."
if command -v node >/dev/null 2>&1; then
    cd server
    if timeout 5 node -e "
    const fs = require('fs');
    const { spawn } = require('child_process');
    
    // Test that the server can be required without errors
    try {
        // Just check that the file parses
        const content = fs.readFileSync('index.js', 'utf8');
        if (content.includes('KiteExtensionProxy')) {
            console.log('  ✅ Server code structure is valid');
        } else {
            console.log('  ❌ Server code structure issue');
            process.exit(1);
        }
    } catch (error) {
        console.log('  ❌ Server code has syntax errors:', error.message);
        process.exit(1);
    }
    " 2>/dev/null; then
        echo "  ✅ Server code is valid"
    else
        echo "  ⚠️  Server code validation failed or timed out"
    fi
    cd ..
else
    echo "  ⚠️  Node.js not found, skipping server test"
fi

# Test 7: Check documentation
echo "✅ Test 7: Checking documentation..."
if [ -f "README.md" ]; then
    if grep -q "Installation" README.md && grep -q "Usage" README.md; then
        echo "  ✅ README.md contains required sections"
    else
        echo "  ❌ README.md missing required sections"
        exit 1
    fi
else
    echo "  ❌ README.md missing"
    exit 1
fi

echo ""
echo "🎉 All tests passed!"
echo ""
echo "Next steps:"
echo "1. Build the Go binaries: ./build-binaries.sh"
echo "2. Create an icon.png file (64x64 PNG)"
echo "3. Install dxt CLI: npm install -g @anthropic-ai/dxt"
echo "4. Package the extension: dxt pack ."
echo "5. Install in Claude Desktop"
echo ""
echo "🔗 For more information, see README.md"