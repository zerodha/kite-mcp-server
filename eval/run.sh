#!/usr/bin/env bash
# MCP Tool Selection Eval
# Starts mock server, runs prompts through pi with MCP tools, scores results.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
PROMPTS_FILE="${SCRIPT_DIR}/prompts.jsonl"
RESULTS_FILE="${SCRIPT_DIR}/results.jsonl"
MCP_CONFIG="${SCRIPT_DIR}/mcp.json"
PORT=8989
START_SERVER=false

for arg in "$@"; do
    case "$arg" in
        --start-server) START_SERVER=true ;;
        --port=*) PORT="${arg#--port=}" ;;
    esac
done

cleanup() {
    if [[ -n "${SERVER_PID:-}" ]]; then
        echo "Stopping mock server (PID $SERVER_PID)..."
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    if [[ -n "${CONN_INFO_FILE:-}" ]]; then
        rm -f "$CONN_INFO_FILE"
    fi
}
trap cleanup EXIT

# Start mock server if requested.
if $START_SERVER; then
    echo "Starting mock server on port $PORT..."
    cd "$REPO_DIR"
    CONN_INFO_FILE=$(mktemp)
    go run -tags=testing ./cmd/mockserver -port "$PORT" -json >"$CONN_INFO_FILE" 2>/dev/null &
    SERVER_PID=$!

    # The mock server emits one JSON object before it starts listening. Wait for
    # it and validate the fields before putting the bearer token in mcp.json.
    for _ in {1..50}; do
        if [[ -s "$CONN_INFO_FILE" ]]; then
            break
        fi
        if ! kill -0 "$SERVER_PID" 2>/dev/null; then
            echo "Mock server exited before publishing connection info." >&2
            exit 1
        fi
        sleep 0.1
    done

    if [[ ! -s "$CONN_INFO_FILE" ]]; then
        echo "Timed out waiting for mock server connection info." >&2
        exit 1
    fi

    ENDPOINT=$(jq -er '.endpoint | strings | select(startswith("http://"))' "$CONN_INFO_FILE")
    KITE_MCP_TOKEN=$(jq -er '.token | strings | select(length > 0)' "$CONN_INFO_FILE")
else
    echo "Assuming mock server is already running on port $PORT"
    if [[ -z "${KITE_MCP_TOKEN:-}" ]]; then
        echo "Set KITE_MCP_TOKEN when using an existing mock server." >&2
        exit 1
    fi
    ENDPOINT="http://localhost:${PORT}/mcp"
fi

# Write mcp.json for pi-mcp-adapter.
cat > "$MCP_CONFIG" <<EOF
{
    "mcpServers": {
        "kite": {
            "url": "${ENDPOINT}",
            "auth": "bearer",
            "bearerToken": "${KITE_MCP_TOKEN}",
            "directTools": true,
            "lifecycle": "eager"
        }
    }
}
EOF

echo "MCP config written to $MCP_CONFIG"
echo "Endpoint: $ENDPOINT"
echo ""

# System prompt for eval - instruct the model to only make tool calls.
SYSTEM_PROMPT="You are evaluating Kite MCP tools. For each user message, make exactly ONE tool call that best answers the request. Do not explain or discuss - just call the appropriate tool with the right mode and parameters. After receiving the tool result, respond with a single line: TOOL_CALL: <tool_name> MODE: <mode_value>"

# Run each prompt.
total=0
correct_tool=0
correct_mode=0
correct_both=0

echo "Running $(wc -l < "$PROMPTS_FILE") eval prompts..."
echo "---"
> "$RESULTS_FILE"

while IFS= read -r line; do
    prompt=$(echo "$line" | jq -r '.prompt')
    expected_tool=$(echo "$line" | jq -r '.expected_tool')
    expected_mode=$(echo "$line" | jq -r '.expected_mode')
    total=$((total + 1))

    echo "[$total] $prompt"
    echo "  Expected: $expected_tool/$expected_mode"

    # Run pi with the prompt. Use -p for non-interactive, -ne for no extensions except mcp-adapter.
    # The --mcp flag points to our config.
    result=$(cd "$REPO_DIR" && pi -p "$prompt" \
        --system "$SYSTEM_PROMPT" \
        -ne \
        -e npm:pi-mcp-adapter \
        --mcp "$MCP_CONFIG" \
        --max-turns 2 \
        2>/dev/null || echo "ERROR")

    # Extract tool call from result.
    actual_tool=$(echo "$result" | grep -oP 'TOOL_CALL:\s*\K\S+' | head -1 || echo "unknown")
    actual_mode=$(echo "$result" | grep -oP 'MODE:\s*\K\S+' | head -1 || echo "unknown")

    tool_match=false
    mode_match=false
    both_match=false

    if [[ "$actual_tool" == "$expected_tool" ]]; then
        tool_match=true
        correct_tool=$((correct_tool + 1))
    fi
    if [[ "$actual_mode" == "$expected_mode" ]]; then
        mode_match=true
        correct_mode=$((correct_mode + 1))
    fi
    if $tool_match && $mode_match; then
        both_match=true
        correct_both=$((correct_both + 1))
        echo "  Result: PASS ($actual_tool/$actual_mode)"
    else
        echo "  Result: FAIL (got $actual_tool/$actual_mode)"
    fi

    # Log result.
    echo "{\"prompt\":$(echo "$prompt" | jq -R .), \"expected_tool\":\"$expected_tool\", \"expected_mode\":\"$expected_mode\", \"actual_tool\":\"$actual_tool\", \"actual_mode\":\"$actual_mode\", \"tool_match\":$tool_match, \"mode_match\":$mode_match, \"both_match\":$both_match}" >> "$RESULTS_FILE"

    echo ""
done < "$PROMPTS_FILE"

# Summary.
echo "=== EVAL RESULTS ==="
echo "Total prompts:  $total"
echo "Tool correct:   $correct_tool / $total ($(( correct_tool * 100 / total ))%)"
echo "Mode correct:   $correct_mode / $total ($(( correct_mode * 100 / total ))%)"
echo "Both correct:   $correct_both / $total ($(( correct_both * 100 / total ))%)"
echo ""
echo "Results saved to $RESULTS_FILE"
