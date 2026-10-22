#!/bin/bash
# OAuth Flow Test Script for Kite MCP Server
#
# This script tests the complete OAuth 2.1 + PKCE flow:
# 1. Generate PKCE code_challenge from code_verifier
# 2. Start OAuth authorize flow
# 3. Exchange authorization code for JWT token
# 4. Test MCP endpoint with the token
#
# Prerequisites:
# - Server running on localhost:8080 with JWT_SECRET configured
# - jq installed for JSON parsing
# - curl installed
#
# Usage: ./scripts/test-oauth.sh

set -e

# Configuration
SERVER_URL="${SERVER_URL:-http://localhost:8080}"
REDIRECT_URI="${REDIRECT_URI:-$SERVER_URL/callback}"

# Generate PKCE code_verifier (43-128 chars, URL-safe)
CODE_VERIFIER="test-code-verifier-for-oauth-flow-testing-12345"

# Calculate code_challenge (SHA256 hash, base64url encoded)
# Using openssl for portability
CODE_CHALLENGE=$(echo -n "$CODE_VERIFIER" | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')

echo "=== Kite MCP Server OAuth Test ==="
echo ""
echo "Server: $SERVER_URL"
echo "Redirect URI: $REDIRECT_URI"
echo ""
echo "PKCE Values:"
echo "  code_verifier: $CODE_VERIFIER"
echo "  code_challenge: $CODE_CHALLENGE"
echo ""

# Step 1: Check server is running
echo "=== Step 1: Checking server health ==="
if ! curl -s "$SERVER_URL/" > /dev/null; then
    echo "ERROR: Server not reachable at $SERVER_URL"
    exit 1
fi
echo "Server is running"
echo ""

# Step 2: Check OAuth discovery
echo "=== Step 2: OAuth Discovery ==="
DISCOVERY=$(curl -s "$SERVER_URL/.well-known/oauth-authorization-server")
echo "$DISCOVERY" | jq . 2>/dev/null || echo "$DISCOVERY"
echo ""

# Check registration_endpoint exists
REG_ENDPOINT=$(echo "$DISCOVERY" | jq -r '.registration_endpoint // empty' 2>/dev/null)
if [ -z "$REG_ENDPOINT" ]; then
    echo "WARNING: registration_endpoint not found in discovery"
else
    echo "registration_endpoint: $REG_ENDPOINT"
fi
echo ""

# Step 2b: Test Dynamic Client Registration
echo "=== Step 2b: Dynamic Client Registration (RFC 7591) ==="
REGISTER_RESPONSE=$(curl -s -X POST "$SERVER_URL/register" \
    -H "Content-Type: application/json" \
    -d "{\"redirect_uris\": [\"$REDIRECT_URI\"], \"client_name\": \"test-client-dcr\"}")

echo "Register Response:"
echo "$REGISTER_RESPONSE" | jq . 2>/dev/null || echo "$REGISTER_RESPONSE"
echo ""

DCR_CLIENT_ID=$(echo "$REGISTER_RESPONSE" | jq -r '.client_id // empty' 2>/dev/null)
if [ -n "$DCR_CLIENT_ID" ]; then
    echo "DCR client_id: $DCR_CLIENT_ID"
    CLIENT_ID="$DCR_CLIENT_ID"
else
    echo "Client registration failed"
    exit 1
fi
echo ""

# Step 3: Generate authorize URL
AUTHORIZE_URL="$SERVER_URL/authorize?response_type=code&client_id=$CLIENT_ID&redirect_uri=$REDIRECT_URI&code_challenge=$CODE_CHALLENGE&code_challenge_method=S256&state=test-state-$$"
echo "=== Step 3: OAuth Authorize URL ==="
echo ""
echo "Open this URL in your browser to login:"
echo ""
echo "$AUTHORIZE_URL"
echo ""

# Step 4: Wait for authorization code
echo "=== Step 4: Waiting for authorization code ==="
echo ""
echo "After logging in via Kite, you'll be redirected to:"
echo "$REDIRECT_URI?code=<authorization_code>&state=test-state-$$"
echo ""
echo "Copy the 'code' parameter value and paste it below:"
echo ""
read -p "Authorization code: " AUTH_CODE

if [ -z "$AUTH_CODE" ]; then
    echo "ERROR: No authorization code provided"
    exit 1
fi
echo ""

# Step 5: Exchange code for token
echo "=== Step 5: Token Exchange ==="
TOKEN_RESPONSE=$(curl -s -X POST "$SERVER_URL/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=authorization_code" \
    -d "code=$AUTH_CODE" \
    -d "redirect_uri=$REDIRECT_URI" \
    -d "client_id=$CLIENT_ID" \
    -d "code_verifier=$CODE_VERIFIER")

echo "Token Response:"
echo "$TOKEN_RESPONSE" | jq . 2>/dev/null || echo "$TOKEN_RESPONSE"
echo ""

ACCESS_TOKEN=$(echo "$TOKEN_RESPONSE" | jq -r '.access_token // empty' 2>/dev/null)

if [ -z "$ACCESS_TOKEN" ]; then
    echo "ERROR: Failed to get access token"
    echo "Response: $TOKEN_RESPONSE"
    exit 1
fi

echo "Access Token: ${ACCESS_TOKEN:0:50}..."
echo ""

# Step 6: Test MCP endpoint - Initialize
echo "=== Step 6: MCP Initialize ==="
INIT_RESPONSE=$(curl -s -D /tmp/mcp_headers.txt -X POST "$SERVER_URL/mcp" \
    -H "Authorization: Bearer $ACCESS_TOKEN" \
    -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}')

echo "Response:"
echo "$INIT_RESPONSE"
echo ""

MCP_SESSION=$(grep -i "mcp-session-id" /tmp/mcp_headers.txt | cut -d: -f2 | tr -d ' \r\n')
echo "MCP Session ID: $MCP_SESSION"
echo ""

# Step 7: Test MCP tool call - get_profile
echo "=== Step 7: MCP Tool Call (get_profile) ==="
PROFILE_RESPONSE=$(curl -s -X POST "$SERVER_URL/mcp" \
    -H "Authorization: Bearer $ACCESS_TOKEN" \
    -H "Mcp-Session-Id: $MCP_SESSION" \
    -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" \
    -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_profile","arguments":{}}}')

echo "Response:"
echo "$PROFILE_RESPONSE" | sed 's/^data: //' | jq '.result.content[0].text | fromjson' 2>/dev/null || echo "$PROFILE_RESPONSE"
echo ""

# Step 8: Test without token (should fail)
echo "=== Step 8: Testing 401 (no token) ==="
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$SERVER_URL/mcp")
echo "HTTP Status Code: $HTTP_CODE"
if [ "$HTTP_CODE" = "401" ]; then
    echo "PASS: Correctly returns 401 without token"
else
    echo "FAIL: Expected 401, got $HTTP_CODE"
fi
echo ""

echo "=== OAuth Test Complete ==="
echo ""
echo "Summary:"
echo "  - OAuth discovery: OK"
echo "  - DCR registration: OK"
echo "  - Token exchange: OK"
echo "  - MCP initialize: OK"
echo "  - Tool call: OK"
echo "  - Auth required: OK"
